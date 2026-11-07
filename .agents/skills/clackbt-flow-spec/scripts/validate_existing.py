"""Additional checks for an evidence-backed contract inventory of an existing TUI.

Loaded by validate.py; Python 3.9+ stdlib only. This module deliberately does not
run or import application code being inspected.
"""
from __future__ import annotations

import hashlib
import re
from pathlib import Path
from typing import Any

VERSION = "1.0.0"
BROWNFIELD_REQUIRED = (
    "CONTRACTS.md", "DECISIONS.md", "EVIDENCE.md", "manifest.json",
    "contracts.json", "evidence.json",
)
DOC_SUFFIXES = (".md", ".rst", ".adoc", ".txt")
SOURCE_KINDS = ("code", "test", "config", "doc")
GENERATED_PREFIX = (".work", "flow-spec")
_TOKEN = re.compile(r"\{\{[^{}]+\}\}")
_MARKER = re.compile(r"\[(?:TODO|TBD)\]|\b(?:TODO|TBD)\b")  # case-sensitive: "todo" is a normal word
_FENCE = re.compile(r"```.*?```|~~~.*?~~~", re.S)
_CODE_SPAN = re.compile(r"`[^`\n]*`")
_DECISION_ID = re.compile(r"\bQ-?(\d+)\b")
EVIDENCE_PREFIXES = (
    "ui.components", "state.fields", "state.invariants", "events",
    "commands", "data.models", "data.sources", "validation.rules",
)


def placeholder_hits(filename: str, body: str) -> bool:
    """True when a ready artifact still contains template tokens or TODO/TBD markers.

    Template tokens ({{...}}) are checked everywhere. TODO/TBD markers are checked only
    in Markdown prose (outside code fences and code spans) and only in upper case, so
    ordinary words such as a "todo" task status or a cited `// TODO` comment are not
    reported as unfinished work.
    """
    if _TOKEN.search(body):
        return True
    if filename.lower().endswith(".md"):
        prose = _CODE_SPAN.sub("", _FENCE.sub("", body))
        return bool(_MARKER.search(prose))
    return False


def decision_ids(text: str) -> set:
    """Numeric decision IDs found in text; accepts canonical Q-1 and legacy Q1 spellings."""
    return {int(n) for n in _DECISION_ID.findall(text)}


def _is_doc(src: dict) -> bool:
    kind = src.get("kind")
    if kind is not None:
        return kind == "doc"
    return str(src.get("path", "")).lower().endswith(DOC_SUFFIXES)


def _items(obj: Any, field: str, label: str, check: Any) -> list:
    if not isinstance(obj, dict) or not isinstance(obj.get(field), list):
        check.error(f"{label}.{field}: expected array")
        return []
    return obj[field]


def _object(value: Any, label: str, check: Any) -> dict:
    if not isinstance(value, dict):
        check.error(f"{label}: expected object")
        return {}
    return value


def _unique(rows: list, key: str, label: str, check: Any) -> dict:
    found = {}
    for i, raw in enumerate(rows):
        obj = _object(raw, f"{label}[{i}]", check)
        value = obj.get(key)
        if not isinstance(value, str) or not value.strip():
            check.error(f"{label}[{i}].{key}: expected nonempty string")
        elif value in found:
            check.error(f"{label}: duplicate {key} {value!r}")
        else:
            found[value] = obj
    return found


def contract_index(contracts: dict, check: Any) -> tuple[dict[str, dict], dict]:
    """Perform internal checks usable without a screen tree or FSM."""
    if contracts.get("schema_version") != VERSION:
        check.error(f"contracts.json: schema_version must equal {VERSION}")
    ui = _object(contracts.get("ui"), "contracts.ui", check)
    state = _object(contracts.get("state"), "contracts.state", check)
    data = _object(contracts.get("data"), "contracts.data", check)
    validation = _object(contracts.get("validation"), "contracts.validation", check)
    components = _unique(_items(ui, "components", "ui", check), "id", "ui.components", check)
    fields = _unique(_items(state, "fields", "state", check), "name", "state.fields", check)
    invariants = _unique(_items(state, "invariants", "state", check), "id", "state.invariants", check)
    events = _unique(_items(contracts, "events", "contracts", check), "id", "events", check)
    commands = _unique(_items(contracts, "commands", "contracts", check), "id", "commands", check)
    models = _unique(_items(data, "models", "data", check), "name", "data.models", check)
    sources = _unique(_items(data, "sources", "data", check), "id", "data.sources", check)
    rules = _unique(_items(validation, "rules", "validation", check), "id", "validation.rules", check)
    index = {}
    for prefix, namespace in (("ui.components", components), ("state.fields", fields),
                              ("state.invariants", invariants), ("events", events),
                              ("commands", commands), ("data.models", models),
                              ("data.sources", sources), ("validation.rules", rules)):
        for name, value in namespace.items():
            index[f"{prefix}.{name}"] = value

    for ident, comp in components.items():
        for field_name in ("screen", "node_id", "kind"):
            if not isinstance(comp.get(field_name), str) or not comp[field_name].strip():
                check.error(f"ui.components.{ident}: missing {field_name}")
        props = _unique(_items(comp, "props", f"ui.components.{ident}", check), "name", f"ui.components.{ident}.props", check)
        for name, prop in props.items():
            if not isinstance(prop.get("type"), str) or not prop["type"].strip():
                check.error(f"ui.components.{ident}.{name}: missing type")
            if not isinstance(prop.get("required"), bool):
                check.error(f"ui.components.{ident}.{name}: required must be boolean")
            source = prop.get("source")
            if not isinstance(source, str) or not (source in fields or source in sources or source.startswith("literal:")):
                check.error(f"ui.components.{ident}.{name}: unresolvable prop source {source!r}")
        for event in _items(comp, "emits", f"ui.components.{ident}", check):
            if event not in events:
                check.error(f"ui.components.{ident}: missing event {event!r}")
            elif events[event].get("origin") != "ui":
                check.error(f"ui.components.{ident}: emitted event {event!r} must be UI-origin")

    for ident, field in fields.items():
        if not isinstance(field.get("type"), str) or not field["type"].strip():
            check.error(f"state.fields.{ident}: missing type")
        if "initial" not in field:
            check.error(f"state.fields.{ident}: missing initial")
        if not isinstance(field.get("scope"), str) or not field["scope"].strip():
            check.error(f"state.fields.{ident}: invalid scope")
        if not isinstance(field.get("description"), str) or not field["description"].strip():
            check.error(f"state.fields.{ident}: missing description")
    for ident, inv in invariants.items():
        if not isinstance(inv.get("expression"), str) or not inv["expression"].strip():
            check.error(f"state.invariants.{ident}: missing expression")

    model_fields = {}
    for ident, model in models.items():
        members = _unique(_items(model, "fields", f"data.models.{ident}", check), "name", f"data.models.{ident}.fields", check)
        model_fields[ident] = set(members)
        for name, field in members.items():
            if not isinstance(field.get("type"), str) or not field["type"].strip():
                check.error(f"data.models.{ident}.{name}: missing type")
            if not isinstance(field.get("required"), bool):
                check.error(f"data.models.{ident}.{name}: required must be boolean")
    for ident, source in sources.items():
        if source.get("model") not in models:
            check.error(f"data.sources.{ident}: missing model {source.get('model')!r}")
        if source.get("cardinality") not in ("one", "many"):
            check.error(f"data.sources.{ident}: cardinality must be one/many")
        if not isinstance(source.get("provider"), str) or not source["provider"].strip():
            check.error(f"data.sources.{ident}: provider required")
    for ident, event in events.items():
        if event.get("origin") not in ("ui", "command", "system"):
            check.error(f"events.{ident}: invalid origin")
        if not isinstance(event.get("payload"), dict):
            check.error(f"events.{ident}: payload must be object")
        else:
            for name, typ in event["payload"].items():
                if not isinstance(typ, str) or not typ.strip():
                    check.error(f"events.{ident}.payload.{name}: invalid type")
    for ident, cmd in commands.items():
        if cmd.get("trigger") not in events:
            check.error(f"commands.{ident}: missing trigger {cmd.get('trigger')!r}")
        outcomes = _items(cmd, "outcomes", f"commands.{ident}", check)
        if not outcomes:
            check.error(f"commands.{ident}: missing outcome events")
        for outcome in outcomes:
            if outcome not in events:
                check.error(f"commands.{ident}: missing outcome {outcome!r}")
            elif events[outcome].get("origin") != "command":
                check.error(f"commands.{ident}: outcome {outcome!r} must be command-origin")
        if not isinstance(cmd.get("input"), dict):
            check.error(f"commands.{ident}: input must be object")
        if not isinstance(cmd.get("side_effect"), str) or not cmd["side_effect"].strip():
            check.error(f"commands.{ident}: missing side_effect description")
        if not isinstance(cmd.get("idempotent"), bool):
            check.error(f"commands.{ident}: idempotent must be boolean")
    for ident, rule in rules.items():
        target = rule.get("target")
        if not isinstance(target, str):
            check.error(f"validation.rules.{ident}: invalid target")
        elif "." in target:
            model, fld = target.split(".", 1)
            if fld not in model_fields.get(model, set()):
                check.error(f"validation.rules.{ident}: invalid model field {target!r}")
        elif target not in fields:
            check.error(f"validation.rules.{ident}: invalid state field {target!r}")
        if rule.get("when") not in events and rule.get("when") not in ("on_submit", "on_change", "on_blur", "on_load"):
            check.error(f"validation.rules.{ident}: invalid trigger {rule.get('when')!r}")
        for k in ("predicate", "message"):
            if not isinstance(rule.get(k), str) or not rule[k].strip():
                check.error(f"validation.rules.{ident}: missing {k}")
    return index, {"events": events, "commands": commands, "fields": fields,
                   "models": models, "sources": sources, "components": components}


def verify_evidence(path: Path, repo_root: Path, contracts: dict, manifest: dict,
                    check: Any, load_json: Any, extra_refs: Any = None) -> None:
    """Check provenance claims. ``extra_refs`` lists additional refs (screens.*,
    machine.states.*, machine.transitions.*) that full-mode reconstructions must back."""
    evidence = load_json(path / "evidence.json", check)
    if evidence.get("schema_version") != VERSION:
        check.error(f"evidence.json: schema_version must equal {VERSION}")
    index, _ = contract_index_for_evidence(contracts)
    extra = set(extra_refs or ())
    claims = _unique(_items(evidence, "claims", "evidence", check), "ref", "evidence.claims", check)
    status = manifest.get("status")
    expected = set(index)
    for ref in sorted(expected - set(claims)):
        check.error(f"evidence.json: missing provenance claim for {ref}")
    for ref in sorted(extra - set(claims)):
        # Reconstructed screens/states/transitions must be labelled; hard error once ready.
        (check.error if status == "ready" else check.warn)(f"evidence.json: missing provenance claim for {ref}")
    for ref in sorted(set(claims) - expected - extra):
        check.error(f"evidence.json: unknown contract ref {ref}")
    for ref, claim in claims.items():
        classification = claim.get("classification")
        if classification not in ("observed", "user-confirmed", "inferred", "proposed"):
            check.error(f"evidence.{ref}: invalid classification {classification!r}")
        if not isinstance(claim.get("rationale"), str) or not claim["rationale"].strip():
            check.error(f"evidence.{ref}: rationale required")
        sources = _items(claim, "sources", f"evidence.{ref}", check)
        if classification == "observed" and not sources:
            check.error(f"evidence.{ref}: observed claim requires file/line provenance")
        if classification == "user-confirmed":
            rationale = claim.get("rationale", "")
            cited = decision_ids(rationale if isinstance(rationale, str) else "")
            log = (path / "DECISIONS.md").read_text(encoding="utf-8") if (path / "DECISIONS.md").is_file() else ""
            logged = decision_ids(log)
            if not cited or not cited <= logged:
                check.error(f"evidence.{ref}: user-confirmed claim must cite Q-<number> entries that all exist in DECISIONS.md")
        if status == "ready" and classification in ("inferred", "proposed"):
            check.error(f"evidence.{ref}: {classification} claims prevent ready status")
        parsed = [_object(raw, f"evidence.{ref}.sources[{i}]", check) for i, raw in enumerate(sources)]
        if classification == "observed" and parsed and all(_is_doc(x) for x in parsed):
            check.error(f"evidence.{ref}: observed claim needs at least one code/test/config source; documentation alone is not proof")
        for i, src in enumerate(parsed):
            kind = src.get("kind")
            if kind is not None and kind not in SOURCE_KINDS:
                check.error(f"evidence.{ref}: invalid source kind {kind!r}")
            rel = src.get("path")
            if not isinstance(rel, str) or not rel.strip() or Path(rel).is_absolute() or ".." in Path(rel).parts or "\\" in rel:
                check.error(f"evidence.{ref}: invalid repo-relative source path {rel!r}")
                continue
            if Path(rel).name.startswith(".env") or ".git" in Path(rel).parts:
                check.error(f"evidence.{ref}: source path must not reference secret/VCS metadata")
                continue
            if Path(rel).parts[:len(GENERATED_PREFIX)] == GENERATED_PREFIX:
                check.error(f"evidence.{ref}: generated design artifacts cannot be cited as source evidence: {rel!r}")
                continue
            file_path = (repo_root / rel).resolve()
            if not file_path.is_relative_to(repo_root.resolve()):
                check.error(f"evidence.{ref}: source path escapes repository: {rel!r}")
                continue
            if not file_path.is_file():
                check.error(f"evidence.{ref}: missing source file {rel!r}")
                continue
            a, b = src.get("line_start"), src.get("line_end")
            if (not isinstance(a, int) or isinstance(a, bool) or not isinstance(b, int)
                    or isinstance(b, bool) or a < 1 or b < a):
                check.error(f"evidence.{ref}: invalid line range {a!r}-{b!r}")
                continue
            try:
                raw = file_path.read_bytes()
                line_count = len(raw.splitlines())
            except OSError as exc:
                check.error(f"evidence.{ref}: cannot read {rel!r}: {exc}")
                continue
            if b > line_count:
                check.error(f"evidence.{ref}: source range {a}-{b} exceeds {rel!r} ({line_count} lines)")
            if status == "ready" and classification == "observed" and "sha256" not in src:
                check.error(f"evidence.{ref}: ready baseline requires sha256 on observed source {rel!r} so drift is detectable")
            if "sha256" in src:
                digest = src["sha256"]
                if not isinstance(digest, str) or not re.fullmatch(r"[0-9a-f]{64}", digest):
                    check.error(f"evidence.{ref}: invalid sha256 digest")
                elif hashlib.sha256(raw).hexdigest() != digest:
                    check.error(f"evidence.{ref}: source drift detected for {rel!r} (sha256 mismatch)")
    gaps = _unique(_items(evidence, "gaps", "evidence", check), "id", "evidence.gaps", check)
    for ident, gap in gaps.items():
        if not isinstance(gap.get("question"), str) or not gap["question"].strip():
            check.error(f"evidence.gaps.{ident}: question required")
        if gap.get("status") not in ("open", "answered", "out-of-scope"):
            check.error(f"evidence.gaps.{ident}: invalid status")
        if gap.get("status") == "open" and status == "ready":
            check.error(f"evidence.gaps.{ident}: open gap prevents ready status")
        if gap.get("status") in ("answered", "out-of-scope") and not gap.get("resolution"):
            check.error(f"evidence.gaps.{ident}: closed gap requires resolution")
        impacts = gap.get("impact_refs", [])
        if not isinstance(impacts, list):
            check.error(f"evidence.gaps.{ident}: impact_refs must be an array")
            impacts = []
        for ref in impacts:
            if not isinstance(ref, str) or (ref not in index and ref not in extra):
                check.error(f"evidence.gaps.{ident}: unknown impacted contract {ref!r}")


def contract_index_for_evidence(contracts: dict) -> tuple[dict[str, dict], dict]:
    index = {}
    def records(obj: Any, key: str) -> list:
        v = obj.get(key, []) if isinstance(obj, dict) else []
        return v if isinstance(v, list) else []
    groups = [
        ("ui.components", records(contracts.get("ui"), "components"), "id"),
        ("state.fields", records(contracts.get("state"), "fields"), "name"),
        ("state.invariants", records(contracts.get("state"), "invariants"), "id"),
        ("events", records(contracts, "events"), "id"),
        ("commands", records(contracts, "commands"), "id"),
        ("data.models", records(contracts.get("data"), "models"), "name"),
        ("data.sources", records(contracts.get("data"), "sources"), "id"),
        ("validation.rules", records(contracts.get("validation"), "rules"), "id"),
    ]
    for name, group, key in groups:
        for item in group:
            if isinstance(item, dict) and isinstance(item.get(key), str):
                index[f"{name}.{item[key]}"] = item
    return index, {}


def validate_contract_only(path: Path, feature: str, repo_root: Path, manifest: dict,
                           check: Any, load_json: Any) -> None:
    if manifest.get("schema_version") != VERSION:
        check.error(f"manifest.json: schema_version must equal {VERSION}")
    if manifest.get("feature_id") != feature:
        check.error("manifest.json: feature_id must match folder slug")
    if manifest.get("source_mode") != "existing-app" or manifest.get("artifact_scope") != "contracts-only":
        check.error("contracts-only mode requires existing-app source_mode")
    if manifest.get("status") not in ("draft", "ready"):
        check.error("manifest.status: expected draft or ready")
    for field in ("title", "source_brief"):
        if not isinstance(manifest.get(field), str) or not manifest[field].strip():
            check.error(f"manifest.{field}: expected nonempty string")
    for field in ("open_questions", "assumptions", "files"):
        if not isinstance(manifest.get(field), list):
            check.error(f"manifest.{field}: expected list")
    if manifest.get("status") == "ready" and manifest.get("open_questions"):
        check.error("ready feature cannot have unresolved open_questions")
    listed = manifest.get("files", [])
    for filename in BROWNFIELD_REQUIRED:
        if filename not in listed:
            check.error(f"manifest.files: missing {filename}")
    if "validation-report.json" not in listed:
        check.warn("manifest.files should list validation-report.json")
    for name in listed:
        if not isinstance(name, str) or not name or Path(name).is_absolute() or ".." in Path(name).parts or "/" in name or "\\" in name:
            check.error(f"manifest.files: invalid filename {name!r}")
    for name in ("CONTRACTS.md", "DECISIONS.md", "EVIDENCE.md"):
        p = path / name
        if not p.is_file() or not p.read_text(encoding="utf-8").strip():
            check.error(f"missing or empty Markdown file: {name}")
    for name in ("contracts.json", "evidence.json"):
        if not (path / name).is_file():
            check.error(f"missing required file: {name}")
    contracts = load_json(path / "contracts.json", check)
    contract_index(contracts, check)
    verify_evidence(path, repo_root, contracts, manifest, check, load_json)
    if manifest.get("status") == "ready":
        for name in BROWNFIELD_REQUIRED:
            p = path / name
            if p.is_file() and placeholder_hits(name, p.read_text(encoding="utf-8")):
                check.error(f"{name}: has unresolved placeholder or TODO/TBD in ready spec")
