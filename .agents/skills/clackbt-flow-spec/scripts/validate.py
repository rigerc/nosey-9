#!/usr/bin/env python3
"""Validate a generated TUI feature bundle using Python 3.9+ stdlib.

Usage:
  python3 validate.py --repo-root /path/to/repo --feature my-feature --write-report

This checks structural consistency and cross-file references. It is not a
runtime UI simulator or comprehensive JSON Schema/type/accessibility checker.
"""
from __future__ import annotations
import argparse
import datetime as dt
import json
import os
import re
import sys
import tempfile
from collections import defaultdict, deque
from pathlib import Path
from typing import Any
from validate_existing import placeholder_hits, validate_contract_only, verify_evidence

VERSION = "1.0.0"
SLUG = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*$")
REQUIRED = ("SPEC.md", "FLOWS.md", "CONTRACTS.md", "DECISIONS.md",
            "manifest.json", "screens.json", "machine.json", "contracts.json")


class Index(dict):
    """dict that treats unhashable lookups (malformed JSON values) as simple misses."""

    def __contains__(self, key: Any) -> bool:
        try:
            return dict.__contains__(self, key)
        except TypeError:
            return False

    def get(self, key: Any, default: Any = None) -> Any:
        try:
            return dict.get(self, key, default)
        except TypeError:
            return default


def hashable(value: Any) -> Any:
    return value if isinstance(value, (str, int, float, bool, type(None))) else repr(value)


_KEY_ALIASES = {"control": "ctrl", "ctl": "ctrl", "option": "alt", "opt": "alt",
                "cmd": "super", "command": "super", "win": "super", "meta": "alt"}


def normalize_key(raw: str) -> str:
    """Canonical key identity: modifier names are case-insensitive and ordered, named keys
    (Enter, Esc, F1) are case-insensitive, but a bare single character keeps its case so
    vim-style ``g`` and ``G`` stay distinct. With Ctrl the letter case is not distinct."""
    text = raw.strip()
    if text in ("", "+"):
        return text
    if text.endswith("++"):
        mods, base = text[:-2].split("+"), "+"
    else:
        *mods, base = text.split("+")
    mods = sorted({_KEY_ALIASES.get(m.strip().lower(), m.strip().lower()) for m in mods if m.strip()})
    base = base.strip()
    if len(base) != 1:
        base = base.lower()
    elif "ctrl" in mods or "alt" in mods:
        base = base.lower()
    elif "shift" in mods and base.isalpha():
        base = base.upper()
        mods.remove("shift")
    return "+".join([*mods, base])


class Check:
    def __init__(self) -> None:
        self.errors: list[str] = []
        self.warnings: list[str] = []

    def error(self, message: str) -> None:
        self.errors.append(message)

    def warn(self, message: str) -> None:
        self.warnings.append(message)

    def expect(self, condition: bool, message: str) -> bool:
        if not condition:
            self.error(message)
        return condition


def object_at(value: Any, where: str, check: Check) -> dict:
    if not isinstance(value, dict):
        check.error(f"{where}: expected object")
        return {}
    return value


def array_at(value: Any, where: str, check: Check) -> list:
    if not isinstance(value, list):
        check.error(f"{where}: expected array")
        return []
    return value


def string_at(obj: dict, field: str, where: str, check: Check, *, empty: bool = False) -> str:
    value = obj.get(field)
    if not isinstance(value, str) or (not empty and not value.strip()):
        check.error(f"{where}.{field}: expected {'string' if empty else 'nonempty string'}")
        return ""
    return value


def unique_entries(rows: list, section: str, check: Check) -> dict[str, dict]:
    result = Index()
    for i, entry in enumerate(rows):
        where = f"{section}[{i}]"
        obj = object_at(entry, where, check)
        ident = string_at(obj, "id", where, check)
        if ident:
            if ident in result:
                check.error(f"{where}: duplicate id {ident!r} in {section}")
            else:
                result[ident] = obj
    return result


def load_json(path: Path, check: Check) -> dict:
    if path.is_symlink():
        check.error(f"{path.name}: refusing symlinked input file")
        return {}
    if not path.is_file():
        check.error(f"missing required file: {path.name}")
        return {}
    try:
        return object_at(json.loads(path.read_text(encoding="utf-8")), path.name, check)
    except (OSError, ValueError) as exc:
        check.error(f"{path.name}: cannot parse JSON: {exc}")
        return {}


def check_nodes(screen_id: str, roots: list, check: Check) -> dict[str, dict]:
    nodes: dict[str, dict] = {}

    def visit(value: Any, path: str, depth: int = 0) -> None:
        if depth > 40:
            check.error(f"{path}: layout tree exceeds depth limit (40)")
            return
        n = object_at(value, path, check)
        node_id = string_at(n, "id", path, check)
        string_at(n, "kind", path, check)
        if node_id:
            if node_id in nodes:
                check.error(f"screen {screen_id}: duplicate node id {node_id!r}")
            else:
                nodes[node_id] = n
        if "focusable" in n:
            check.expect(isinstance(n["focusable"], bool), f"{path}.focusable: expected boolean")
        if "on" in n:
            on = object_at(n["on"], path + ".on", check)
            for gesture, event in on.items():
                check.expect(isinstance(gesture, str) and bool(gesture.strip()), f"{path}.on: empty gesture")
                check.expect(isinstance(event, str) and bool(event.strip()), f"{path}.on[{gesture}]: invalid event")
        for child in array_at(n.get("children", []), path + ".children", check):
            visit(child, path + ".children", depth + 1)
    for i, root in enumerate(roots):
        visit(root, f"screens[{screen_id}].regions[{i}]")
    return nodes


def validate_folder(path: Path, feature: str, repo_root: Path | None = None) -> Check:
    check = Check()
    if not path.is_dir():
        check.error(f"feature folder does not exist: {path}")
        return check

    if repo_root is None:
        repo_root = path.parents[4]
    manifest = load_json(path / "manifest.json", check)
    source_mode = manifest.get("source_mode", "brief")
    scope = manifest.get("artifact_scope", "full")
    check.expect(source_mode in ("brief", "existing-app"), "manifest.source_mode: invalid mode")
    check.expect(scope in ("full", "contracts-only"), "manifest.artifact_scope: invalid scope")
    if scope == "contracts-only":
        validate_contract_only(path, feature, repo_root, manifest, check, load_json)
        return check

    for filename in REQUIRED:
        if filename.endswith(".md"):
            p = path / filename
            if not p.is_file() or not p.read_text(encoding="utf-8").strip():
                check.error(f"missing or empty Markdown file: {filename}")
            elif not p.read_text(encoding="utf-8").lstrip().startswith("#"):
                check.warn(f"{filename}: recommended Markdown heading is missing")

    screen_doc = load_json(path / "screens.json", check)
    machine_doc = load_json(path / "machine.json", check)
    contracts = load_json(path / "contracts.json", check)
    for filename, content in (("manifest.json", manifest), ("screens.json", screen_doc),
                              ("machine.json", machine_doc), ("contracts.json", contracts)):
        check.expect(content.get("schema_version") == VERSION,
                     f"{filename}: schema_version must equal {VERSION}")

    check.expect(manifest.get("feature_id") == feature, "manifest.json: feature_id must match folder slug")
    string_at(manifest, "title", "manifest", check)
    string_at(manifest, "source_brief", "manifest", check)
    status = manifest.get("status")
    check.expect(status in ("draft", "ready"), "manifest.status: expected draft or ready")
    entry_screen = string_at(manifest, "entry_screen", "manifest", check)
    open_questions = array_at(manifest.get("open_questions"), "manifest.open_questions", check)
    assumptions = array_at(manifest.get("assumptions"), "manifest.assumptions", check)
    if status == "ready" and open_questions:
        check.error("ready feature cannot have unresolved open_questions")
    for i, v in enumerate(open_questions + assumptions):
        check.expect(isinstance(v, str) and bool(v.strip()), f"manifest: question/assumption {i} must be nonempty text")
    listed_files = array_at(manifest.get("files"), "manifest.files", check)
    for filename in REQUIRED:
        check.expect(filename in listed_files, f"manifest.files: missing {filename}")
    if "validation-report.json" not in listed_files:
        check.warn("manifest.files should list validation-report.json")
    for f in listed_files:
        if not isinstance(f, str) or f.startswith("/") or ".." in Path(f).parts or "/" in f or "\\" in f:
            check.error(f"manifest.files: invalid top-level feature filename {f!r}")

    screens = unique_entries(array_at(screen_doc.get("screens"), "screens.screens", check), "screens", check)
    states = unique_entries(array_at(machine_doc.get("states"), "machine.states", check), "machine.states", check)
    guards = unique_entries(array_at(machine_doc.get("guards"), "machine.guards", check), "machine.guards", check)
    transitions = unique_entries(array_at(machine_doc.get("transitions"), "machine.transitions", check), "machine.transitions", check)
    events = unique_entries(array_at(contracts.get("events"), "contracts.events", check), "contracts.events", check)
    commands = unique_entries(array_at(contracts.get("commands"), "contracts.commands", check), "contracts.commands", check)
    ui = object_at(contracts.get("ui"), "contracts.ui", check)
    ui_components = unique_entries(array_at(ui.get("components"), "contracts.ui.components", check), "contracts.ui.components", check)
    state_contract = object_at(contracts.get("state"), "contracts.state", check)
    state_fields = unique_entries_by_name(array_at(state_contract.get("fields"), "contracts.state.fields", check), "contracts.state.fields", check)
    unique_entries(array_at(state_contract.get("invariants"), "contracts.state.invariants", check), "contracts.state.invariants", check)
    data = object_at(contracts.get("data"), "contracts.data", check)
    models = unique_entries_by_name(array_at(data.get("models"), "contracts.data.models", check), "contracts.data.models", check)
    sources = unique_entries(array_at(data.get("sources"), "contracts.data.sources", check), "contracts.data.sources", check)
    validation = object_at(contracts.get("validation"), "contracts.validation", check)
    rules = unique_entries(array_at(validation.get("rules"), "contracts.validation.rules", check), "contracts.validation.rules", check)

    check.expect(entry_screen in screens, f"manifest.entry_screen: unknown screen {entry_screen!r}")
    initial = string_at(machine_doc, "initial", "machine", check)
    check.expect(initial in states, f"machine.initial: unknown state {initial!r}")
    if initial in states:
        check.expect(states[initial].get("screen") == entry_screen,
                     "machine initial state must belong to manifest entry screen")

    node_by_screen = {}
    for screen_id, screen in screens.items():
        string_at(screen, "title", f"screens.{screen_id}", check)
        string_at(screen, "purpose", f"screens.{screen_id}", check)
        min_size = object_at(screen.get("min_size"), f"screens.{screen_id}.min_size", check)
        for dimension in ("cols", "rows"):
            v = min_size.get(dimension)
            check.expect(isinstance(v, int) and not isinstance(v, bool) and v > 0,
                         f"screens.{screen_id}.min_size.{dimension}: expected positive integer")
        presentation = screen.get("presentation", "fullscreen")
        check.expect(presentation in ("fullscreen", "inline"),
                     f"screens.{screen_id}.presentation: expected fullscreen or inline")
        cli = screen.get("cli")
        if presentation == "inline":
            if isinstance(cli, dict):
                string_at(cli, "invocation", f"screens.{screen_id}.cli", check)
                check.expect(cli.get("interaction") in ("batch", "prompted"),
                             f"screens.{screen_id}.cli.interaction: expected batch or prompted")
                check.expect(cli.get("stdin") in ("none", "pipe", "tty"),
                             f"screens.{screen_id}.cli.stdin: expected none, pipe or tty")
                if cli.get("interaction") == "prompted":
                    check.expect(cli.get("stdin") == "tty",
                                 f"screens.{screen_id}.cli: prompted interaction requires tty stdin")
                for extra in cli.keys() - {"invocation", "interaction", "stdin"}:
                    check.error(f"screens.{screen_id}.cli: unsupported property {extra!r}")
            else:
                check.error(f"screens.{screen_id}: inline presentation requires cli metadata")
        elif cli is not None:
            check.error(f"screens.{screen_id}: cli metadata requires inline presentation")
        nodes = check_nodes(screen_id, array_at(screen.get("regions"), f"screens.{screen_id}.regions", check), check)
        node_by_screen[screen_id] = nodes
        for nid, node in nodes.items():
            kind, stream = node.get("kind"), node.get("stream")
            if stream is not None:
                check.expect(kind == "output" and stream in ("stdout", "stderr"),
                             f"screens.{screen_id}.{nid}: stream only supports output stdout/stderr")
            if kind in ("command", "output", "prompt", "spinner", "progress"):
                check.expect(presentation == "inline",
                             f"screens.{screen_id}.{nid}: CLI node {kind!r} requires inline presentation")
            if kind == "prompt" and isinstance(cli, dict):
                check.expect(cli.get("interaction") == "prompted",
                             f"screens.{screen_id}.{nid}: prompt requires prompted interaction")
        focus_order = array_at(screen.get("focus_order"), f"screens.{screen_id}.focus_order", check)
        if not all(isinstance(x, str) for x in focus_order):
            check.error(f"screens.{screen_id}: focus_order entries must be node ID strings")
        focus_order = [x for x in focus_order if isinstance(x, str)]
        if len(focus_order) != len(set(focus_order)):
            check.error(f"screens.{screen_id}: focus_order contains duplicates")
        focusable = {nid for nid, n in nodes.items() if n.get("focusable") is True}
        check.expect(set(focus_order) == focusable,
                     f"screens.{screen_id}: focus_order must list precisely focusable nodes; expected {sorted(focusable)}")
        kb_seen = set()
        for i, kb in enumerate(array_at(screen.get("keybindings"), f"screens.{screen_id}.keybindings", check)):
            k = object_at(kb, f"screens.{screen_id}.keybindings[{i}]", check)
            key = normalize_key(string_at(k, "key", f"screens.{screen_id}.keybindings[{i}]", check))
            binding_scope = k.get("scope", "screen")
            if binding_scope != "screen":
                check.expect(binding_scope in nodes, f"screens.{screen_id}: keybinding scope {binding_scope!r} unknown")
            pair = (str(binding_scope), key)
            if pair in kb_seen:
                check.error(f"screens.{screen_id}: duplicate keybinding {pair}")
            elif binding_scope != "screen" and ("screen", key) in kb_seen:
                check.warn(f"screens.{screen_id}: node-scoped key {key!r} on {binding_scope!r} shadows a screen-level binding")
            kb_seen.add(pair)
            check.expect(k.get("event") in events, f"screens.{screen_id}: keybinding event {k.get('event')!r} undefined")
        screen_states = array_at(screen.get("states"), f"screens.{screen_id}.states", check)
        for s in screen_states:
            check.expect(s in states, f"screens.{screen_id}: references unknown state {s!r}")
            if s in states:
                check.expect(states[s].get("screen") == screen_id,
                             f"screens.{screen_id}: state {s!r} belongs to another screen")
        expected_states = {sid for sid, s in states.items() if s.get("screen") == screen_id}
        check.expect(set(screen_states) == expected_states,
                     f"screens.{screen_id}: states must match machine (expected {sorted(expected_states)})")
        for nid, node in nodes.items():
            if node.get("data_ref") is not None:
                check.expect(node["data_ref"] in sources,
                             f"screens.{screen_id}.{nid}: unknown data_ref {node['data_ref']!r}")
            for event in object_at(node.get("on", {}), f"screens.{screen_id}.{nid}.on", check).values():
                check.expect(event in events, f"screens.{screen_id}.{nid}: unknown UI event {event!r}")
                if event in events:
                    check.expect(events[event].get("origin") == "ui",
                                 f"screens.{screen_id}.{nid}: event {event!r} must have origin ui")

    for state_id, state in states.items():
        check.expect(state.get("screen") in screens,
                     f"machine state {state_id}: unknown screen {state.get('screen')!r}")
        check.expect(isinstance(state.get("terminal"), bool),
                     f"machine state {state_id}: terminal must be boolean")
        string_at(state, "description", f"machine.states.{state_id}", check)
        exit_code = state.get("exit_code")
        presentation = screens.get(state.get("screen"), {}).get("presentation", "fullscreen")
        if presentation == "inline" and state.get("terminal") is True:
            check.expect(isinstance(exit_code, int) and not isinstance(exit_code, bool) and 0 <= exit_code <= 255,
                         f"machine.states.{state_id}: inline terminal state requires exit_code 0-255")
        elif exit_code is not None:
            check.error(f"machine.states.{state_id}: exit_code is only valid for terminal inline states")
    for gid, guard in guards.items():
        string_at(guard, "expression", f"guards.{gid}", check)
        string_at(guard, "description", f"guards.{gid}", check)

    for eid, event in events.items():
        payload = object_at(event.get("payload"), f"events.{eid}.payload", check)
        for name, value in payload.items():
            check.expect(isinstance(value, str) and bool(value.strip()),
                         f"events.{eid}.payload.{name}: expected type string")
        check.expect(event.get("origin") in ("ui", "command", "system"),
                     f"events.{eid}.origin: expected ui, command, or system")
        string_at(event, "description", f"events.{eid}", check)

    for name, field in state_fields.items():
        string_at(field, "type", f"state.fields.{name}", check)
        check.expect("initial" in field, f"state.fields.{name}: missing initial value")
        check.expect(field.get("scope") == "feature" or field.get("scope") in screens,
                     f"state.fields.{name}: invalid scope {field.get('scope')!r}")
        string_at(field, "description", f"state.fields.{name}", check)
    for name, model in models.items():
        fields = unique_entries_by_name(array_at(model.get("fields"), f"data.models.{name}.fields", check), f"data.models.{name}.fields", check)
        for fname, field in fields.items():
            string_at(field, "type", f"data.models.{name}.{fname}", check)
            check.expect(isinstance(field.get("required"), bool), f"data.models.{name}.{fname}: required must be boolean")
            string_at(field, "description", f"data.models.{name}.{fname}", check)
    for sid, source in sources.items():
        check.expect(source.get("model") in models,
                     f"data.sources.{sid}: unknown model {source.get('model')!r}")
        check.expect(source.get("cardinality") in ("one", "many"),
                     f"data.sources.{sid}: cardinality must be one or many")
        string_at(source, "provider", f"data.sources.{sid}", check)
        string_at(source, "description", f"data.sources.{sid}", check)
        if status == "ready" and re.search(r"\b(TBD|TODO|UNKNOWN)\b", str(source.get("provider", "")), re.I):
            check.error(f"data.sources.{sid}: provider unspecified in ready spec")

    for cid, cmd in commands.items():
        trigger = cmd.get("trigger")
        check.expect(trigger in events, f"commands.{cid}: unknown trigger event {trigger!r}")
        outcomes = array_at(cmd.get("outcomes"), f"commands.{cid}.outcomes", check)
        check.expect(bool(outcomes), f"commands.{cid}: must declare outcome events")
        for outcome in outcomes:
            check.expect(outcome in events, f"commands.{cid}: undefined outcome event {outcome!r}")
            if outcome in events:
                check.expect(events[outcome].get("origin") == "command",
                             f"commands.{cid}: outcome event {outcome!r} must have origin command")
        param = object_at(cmd.get("input"), f"commands.{cid}.input", check)
        for key, typ in param.items():
            check.expect(isinstance(typ, str) and bool(typ.strip()), f"commands.{cid}.input.{key}: invalid type")
        string_at(cmd, "side_effect", f"commands.{cid}", check)
        check.expect(isinstance(cmd.get("idempotent"), bool), f"commands.{cid}: idempotent must be boolean")

    for uid, component in ui_components.items():
        screen_id = component.get("screen")
        node_id = component.get("node_id")
        node = node_by_screen.get(screen_id, {}).get(node_id)
        check.expect(node is not None,
                     f"ui.components.{uid}: unknown screen/node ({screen_id!r}, {node_id!r})")
        if node:
            check.expect(node.get("kind") == component.get("kind"),
                         f"ui.components.{uid}: kind differs from screen node")
        props = array_at(component.get("props"), f"ui.components.{uid}.props", check)
        prop_names = set()
        for i, value in enumerate(props):
            p = object_at(value, f"ui.components.{uid}.props[{i}]", check)
            name = string_at(p, "name", f"ui.components.{uid}.props[{i}]", check)
            if name in prop_names:
                check.error(f"ui.components.{uid}: duplicate prop {name!r}")
            prop_names.add(name)
            string_at(p, "type", f"ui.components.{uid}.props[{i}]", check)
            check.expect(isinstance(p.get("required"), bool), f"ui.components.{uid}.{name}: required must be boolean")
            source = string_at(p, "source", f"ui.components.{uid}.props[{i}]", check)
            check.expect(source in sources or source in state_fields or source.startswith("literal:"),
                         f"ui.components.{uid}.{name}: source {source!r} must be data source, state field, or literal:...")
        emits = array_at(component.get("emits"), f"ui.components.{uid}.emits", check)
        for ev in emits:
            check.expect(ev in events, f"ui.components.{uid}: undefined emitted event {ev!r}")
            if ev in events:
                check.expect(events[ev].get("origin") == "ui", f"ui.components.{uid}: emitted event {ev!r} must be UI-origin")
        if node:
            declared = {hashable(v) for v in object_at(node.get("on", {}), "node.on", check).values()}
            emitted = {hashable(v) for v in emits}
            check.expect(declared <= emitted,
                         f"ui.components.{uid}: node 'on' events not all listed in emits: {sorted(map(str, declared - emitted))}")

    ui_by_node = {(hashable(u.get("screen")), hashable(u.get("node_id"))) for u in ui_components.values()}
    for sid, nodes in node_by_screen.items():
        for nid, node in nodes.items():
            if node.get("focusable") or node.get("data_ref") or node.get("on"):
                check.expect((sid, nid) in ui_by_node,
                             f"screens.{sid}.{nid}: interactive/data node has no UI component contract")

    referenced_guard = set()
    outgoing = defaultdict(list)
    graph = defaultdict(list)
    for tid, tr in transitions.items():
        frm, to, event_id = tr.get("from"), tr.get("to"), tr.get("event")
        check.expect(frm in states, f"transitions.{tid}: unknown from {frm!r}")
        check.expect(to in states, f"transitions.{tid}: unknown to {to!r}")
        check.expect(event_id in events, f"transitions.{tid}: unknown event {event_id!r}")
        if frm in states and to in states:
            outgoing[frm].append(tr)
            graph[frm].append(to)
        guard_id = tr.get("guard")
        if guard_id is not None:
            check.expect(guard_id in guards, f"transitions.{tid}: unknown guard {guard_id!r}")
            referenced_guard.add(guard_id)
        cmd_ids = [c for c in array_at(tr.get("commands"), f"transitions.{tid}.commands", check)]
        for cmd_id in cmd_ids:
            check.expect(cmd_id in commands, f"transitions.{tid}: unknown command {cmd_id!r}")
            if cmd_id in commands:
                check.expect(commands[cmd_id].get("trigger") == event_id,
                             f"transitions.{tid}: command {cmd_id!r} trigger must equal transition event {event_id!r}")
        for j, update in enumerate(array_at(tr.get("updates", []), f"transitions.{tid}.updates", check)):
            u = object_at(update, f"transitions.{tid}.updates[{j}]", check)
            check.expect(u.get("field") in state_fields,
                         f"transitions.{tid}.updates[{j}]: unknown state field {u.get('field')!r}")
            string_at(u, "expression", f"transitions.{tid}.updates[{j}]", check)
        string_at(tr, "description", f"transitions.{tid}", check)
    by_trigger = defaultdict(list)
    for tid, tr in transitions.items():
        by_trigger[(hashable(tr.get("from")), hashable(tr.get("event")))].append((tid, tr.get("guard")))
    for (frm_id, event_name), group in by_trigger.items():
        if len(group) < 2:
            continue
        ids = ", ".join(t for t, _ in group)
        guards_used = [hashable(g) for _, g in group]
        if None in guards_used:
            check.error(f"machine: ambiguous transitions from {frm_id!r} on {event_name!r} ({ids}): an unguarded transition coexists with others")
        elif len(set(guards_used)) != len(guards_used):
            check.error(f"machine: nondeterministic transitions from {frm_id!r} on {event_name!r} ({ids}): the same guard is used more than once")
    if initial in states:
        seen = {initial}
        todo = deque([initial])
        while todo:
            for dest in graph[todo.popleft()]:
                if dest not in seen:
                    seen.add(dest)
                    todo.append(dest)
        for state_id in states:
            check.expect(state_id in seen, f"machine: unreachable state {state_id!r}")
    for sid, state in states.items():
        if not state.get("terminal"):
            check.expect(bool(outgoing[sid]), f"machine: nonterminal state {sid!r} has no outgoing transitions")
        elif outgoing[sid]:
            check.error(f"machine: terminal state {sid!r} has outgoing transitions")
    for g in guards:
        if g not in referenced_guard:
            check.warn(f"machine: unused guard {g!r}")
    for tid, tr in transitions.items():
        raw_cmds = tr.get("commands", [])
        for cid in raw_cmds if isinstance(raw_cmds, list) else []:
            if cid in commands:
                target = tr.get("to")
                available = {t.get("event") for t in outgoing[target]}
                for result in commands[cid].get("outcomes", []):
                    check.expect(result in available,
                                 f"transitions.{tid}: command outcome {result!r} not handled in target state {target!r}")

    for sid, nodes in node_by_screen.items():
        generated = []
        for node in nodes.values():
            generated.extend(object_at(node.get("on", {}), "node.on", check).values())
        raw_kbs = screens[sid].get("keybindings", [])
        for kb in raw_kbs if isinstance(raw_kbs, list) else []:
            if isinstance(kb, dict):
                generated.append(kb.get("event"))
        raw_states = screens[sid].get("states", [])
        supported = {hashable(tr.get("event")) for st in (raw_states if isinstance(raw_states, list) else [])
                     if isinstance(st, str) for tr in outgoing[st]}
        for event_id in generated:
            check.expect(hashable(event_id) in supported,
                         f"screens.{sid}: UI event {event_id!r} has no transition from any of this screen's states")

    model_fields = {m: {f.get("name") for f in value.get("fields", []) if isinstance(f, dict)}
                    for m, value in models.items()}
    for rid, rule in rules.items():
        target = string_at(rule, "target", f"validation.{rid}", check)
        if "." in target:
            model, field = target.split(".", 1)
            check.expect(field in model_fields.get(model, set()),
                         f"validation.{rid}: unknown model field target {target!r}")
        else:
            check.expect(target in state_fields, f"validation.{rid}: unknown state field {target!r}")
        when = string_at(rule, "when", f"validation.{rid}", check)
        check.expect(when in events or when in ("on_submit", "on_change", "on_blur", "on_load"),
                     f"validation.{rid}: unknown trigger {when!r}")
        string_at(rule, "predicate", f"validation.{rid}", check)
        string_at(rule, "message", f"validation.{rid}", check)

    referenced_events = set()
    for node_map in node_by_screen.values():
        for node in node_map.values():
            referenced_events.update(hashable(v) for v in object_at(node.get("on", {}), "node.on", check).values())
    for screen in screens.values():
        raw_kbs = screen.get("keybindings", [])
        referenced_events.update(hashable(kb.get("event")) for kb in raw_kbs if isinstance(kb, dict)) if isinstance(raw_kbs, list) else None
    for component in ui_components.values():
        raw_emits = component.get("emits", [])
        referenced_events.update(hashable(e) for e in raw_emits) if isinstance(raw_emits, list) else None
    for cmd in commands.values():
        referenced_events.add(hashable(cmd.get("trigger")))
        raw_out = cmd.get("outcomes", [])
        referenced_events.update(hashable(e) for e in raw_out) if isinstance(raw_out, list) else None
    for rule in rules.values():
        referenced_events.add(hashable(rule.get("when")))
    consumed_events = {hashable(tr.get("event")) for tr in transitions.values()}
    referenced_events |= consumed_events
    for eid, event in events.items():
        if eid not in referenced_events:
            check.warn(f"contracts: event {eid!r} is declared but never emitted, triggered, or handled")
        elif event.get("origin") in ("command", "system") and eid not in consumed_events:
            check.warn(f"contracts: {event.get('origin')} event {eid!r} has no transition that handles it")

    if source_mode == "existing-app":
        for filename in ("EVIDENCE.md", "evidence.json"):
            if filename not in listed_files:
                check.error(f"manifest.files: missing {filename}")
        evidence_md = path / "EVIDENCE.md"
        if not evidence_md.is_file() or not evidence_md.read_text(encoding="utf-8").strip():
            check.error("missing or empty Markdown file: EVIDENCE.md")
        extra_refs = ([f"screens.{k}" for k in screens] + [f"machine.states.{k}" for k in states]
                      + [f"machine.transitions.{k}" for k in transitions])
        verify_evidence(path, repo_root, contracts, manifest, check, load_json, extra_refs)
    if status == "ready":
        for filename in REQUIRED:
            p = path / filename
            if not p.is_file():
                continue
            try:
                body = p.read_text(encoding="utf-8")
            except OSError:
                continue
            if placeholder_hits(filename, body):
                check.error(f"{filename}: has unresolved placeholder or TODO/TBD in ready spec")
    return check


def unique_entries_by_name(rows: list, section: str, check: Check) -> dict[str, dict]:
    result = Index()
    for i, row in enumerate(rows):
        where = f"{section}[{i}]"
        obj = object_at(row, where, check)
        name = string_at(obj, "name", where, check)
        if name:
            if name in result:
                check.error(f"{where}: duplicate name {name!r}")
            else:
                result[name] = obj
    return result


def write_report(destination: Path, text: str) -> None:
    """Write atomically via a fresh temp file so a pre-planted symlink is replaced, never followed."""
    fd, tmp = tempfile.mkstemp(prefix=".report-", suffix=".tmp", dir=str(destination.parent))
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            handle.write(text)
        os.chmod(tmp, 0o644)
        os.replace(tmp, destination)
    except BaseException:
        try:
            os.unlink(tmp)
        except OSError:
            pass
        raise


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--repo-root", required=True, help="Established repository root (not an arbitrary working directory)")
    ap.add_argument("--feature", required=True, help="Lowercase kebab-case feature slug")
    ap.add_argument("--write-report", action="store_true", help="Write validation-report.json within this feature folder")
    args = ap.parse_args(argv)
    if not SLUG.fullmatch(args.feature):
        ap.error("--feature must be lowercase kebab-case")
    root = Path(args.repo_root).expanduser().resolve()
    if not root.is_dir():
        ap.error(f"--repo-root does not exist: {root}")
    expected_parent = root / ".work" / "flow-spec"
    cursor = expected_parent / args.feature
    while cursor != root and root in cursor.parents:
        if cursor.is_symlink():
            ap.error(f"symlink not allowed in generated artifact path: {cursor}")
        cursor = cursor.parent
    target = (expected_parent / args.feature).resolve()
    if target.parent != expected_parent.resolve():
        ap.error("feature folder resolves outside the fixed output directory (possible symlink escape)")

    try:
        result = validate_folder(target, args.feature, root)
    except Exception as exc:  # malformed input must yield a failing report, never a traceback
        result = Check()
        result.error(f"internal validator error ({type(exc).__name__}): {exc}; check the JSON shapes against assets/schemas")
    report = {
        "schema_version": VERSION,
        "feature_id": args.feature,
        "checked_at_utc": dt.datetime.now(dt.timezone.utc).isoformat(),
        "status": "pass" if not result.errors else "fail",
        "checks": "structure and cross-file references only; not runtime correctness or accessibility compliance",
        "error_count": len(result.errors),
        "warning_count": len(result.warnings),
        "errors": result.errors,
        "warnings": result.warnings,
    }
    if args.write_report and target.is_dir():
        write_report(target / "validation-report.json", json.dumps(report, indent=2, ensure_ascii=False) + "\n")
    print(json.dumps(report, indent=2, ensure_ascii=False))
    return 0 if not result.errors else 1


if __name__ == "__main__":
    sys.exit(main())
