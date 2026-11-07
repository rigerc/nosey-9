#!/usr/bin/env python3
"""Regression tests for validator defects fixed in 1.2.1 (stdlib only)."""
from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SKILL_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SKILL_ROOT / "scripts"))
import validate as V  # noqa: E402
import validate_existing as VE  # noqa: E402

SCRIPT = SKILL_ROOT / "scripts" / "validate.py"
TRACKER = "example-task-tracker"
EXISTING = "example-existing-app"
SOURCE_REL = Path(".agents/skills/clackbt-flow-spec/tests/fixtures/existing_app.py")


class Base(unittest.TestCase):
    feature = TRACKER

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.dir = self.root / ".work/flow-spec" / self.feature
        shutil.copytree(SKILL_ROOT / "examples" / self.feature, self.dir)
        if self.feature == EXISTING:
            src = self.root / SOURCE_REL
            src.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy(SKILL_ROOT / "tests/fixtures/existing_app.py", src)

    def tearDown(self):
        self.temp.cleanup()

    def read(self, name):
        return json.loads((self.dir / name).read_text())

    def write(self, name, doc):
        (self.dir / name).write_text(json.dumps(doc, indent=2) + "\n")

    def run_validator(self, *extra):
        p = subprocess.run([sys.executable, str(SCRIPT), "--repo-root", str(self.root), "--feature", self.feature, *extra],
                           capture_output=True, text=True)
        return p

    def validate(self):
        p = self.run_validator()
        try:
            return p, json.loads(p.stdout)
        except ValueError:
            self.fail(f"validator did not emit a JSON report:\n{p.stdout}\n{p.stderr}")


class KeyNormalization(unittest.TestCase):
    def test_case_of_single_characters_is_preserved(self):
        self.assertNotEqual(V.normalize_key("g"), V.normalize_key("G"))

    def test_modifiers_and_named_keys_are_case_insensitive(self):
        self.assertEqual(V.normalize_key("Ctrl+S"), V.normalize_key("ctrl+s"))
        self.assertEqual(V.normalize_key("CTRL+S"), V.normalize_key("control+s"))
        self.assertEqual(V.normalize_key("Enter"), V.normalize_key("enter"))
        self.assertEqual(V.normalize_key("Alt+Shift+X"), V.normalize_key("shift+alt+x"))

    def test_shift_letter_equals_capital(self):
        self.assertEqual(V.normalize_key("Shift+g"), V.normalize_key("G"))

    def test_plus_key(self):
        self.assertEqual(V.normalize_key("+"), "+")
        self.assertEqual(V.normalize_key("Ctrl++"), "ctrl++")


class TrackerRegressions(Base):
    def bindings(self):
        screens = self.read("screens.json")
        event = screens["screens"][0]["keybindings"][1]["event"]
        return screens, event

    def test_vim_style_g_and_capital_g_are_allowed(self):
        screens, event = self.bindings()
        screens["screens"][0]["keybindings"] += [{"key": "g", "event": event}, {"key": "G", "event": event}]
        self.write("screens.json", screens)
        _, report = self.validate()
        self.assertFalse(any("duplicate keybinding" in e for e in report["errors"]), report["errors"])

    def test_ctrl_case_duplicates_are_still_caught(self):
        screens, event = self.bindings()
        screens["screens"][0]["keybindings"] += [{"key": "Ctrl+X", "event": event}, {"key": "ctrl+x", "event": event}]
        self.write("screens.json", screens)
        _, report = self.validate()
        self.assertTrue(any("duplicate keybinding" in e for e in report["errors"]))

    def test_lowercase_todo_word_is_not_a_placeholder(self):
        with (self.dir / "SPEC.md").open("a") as fh:
            fh.write("\nTask statuses: todo, doing, done. Cited marker: `// TODO`.\n")
        p, report = self.validate()
        self.assertEqual(p.returncode, 0, report)

    def test_uppercase_todo_in_prose_still_blocks_ready(self):
        with (self.dir / "SPEC.md").open("a") as fh:
            fh.write("\nTODO: decide the empty-state copy.\n")
        _, report = self.validate()
        self.assertTrue(any("placeholder" in e for e in report["errors"]))

    def test_template_token_still_blocks_ready(self):
        with (self.dir / "FLOWS.md").open("a") as fh:
            fh.write("\n{{state-id}}\n")
        _, report = self.validate()
        self.assertTrue(any("placeholder" in e for e in report["errors"]))

    def test_malformed_values_do_not_crash(self):
        screens = self.read("screens.json")
        screens["screens"][0]["keybindings"][0]["event"] = ["x"]
        screens["screens"][0]["focus_order"] = [["nested"]]
        screens["screens"][0]["states"] = [{"a": 1}]
        self.write("screens.json", screens)
        machine = self.read("machine.json")
        machine["transitions"][0]["event"] = ["bad"]
        machine["transitions"][0]["commands"] = "not-a-list"
        self.write("machine.json", machine)
        p, report = self.validate()
        self.assertEqual(p.returncode, 1)
        self.assertNotIn("Traceback", p.stderr)
        self.assertGreater(report["error_count"], 0)

    def test_identical_transition_is_nondeterministic(self):
        machine = self.read("machine.json")
        dup = dict(machine["transitions"][0], id="duplicate-transition")
        machine["transitions"].append(dup)
        self.write("machine.json", machine)
        _, report = self.validate()
        self.assertTrue(any("nondeterministic" in e or "ambiguous" in e for e in report["errors"]), report["errors"])

    def test_unguarded_alongside_guarded_is_ambiguous(self):
        machine = self.read("machine.json")
        guarded = next(t for t in machine["transitions"] if t["guard"])
        machine["transitions"].append(dict(guarded, id="unguarded-twin", guard=None))
        self.write("machine.json", machine)
        _, report = self.validate()
        self.assertTrue(any("ambiguous" in e for e in report["errors"]), report["errors"])

    def test_distinct_guards_are_accepted(self):
        machine = self.read("machine.json")
        guarded = next(t for t in machine["transitions"] if t["guard"])
        other = next(g["id"] for g in machine["guards"] if g["id"] != guarded["guard"])
        machine["transitions"].append(dict(guarded, id="other-guard-twin", guard=other))
        self.write("machine.json", machine)
        _, report = self.validate()
        self.assertFalse(any("nondeterministic" in e or "ambiguous" in e for e in report["errors"]), report["errors"])

    def test_orphan_event_is_a_warning(self):
        contracts = self.read("contracts.json")
        contracts["events"].append({"id": "orphan.event", "payload": {}, "description": "never used", "origin": "system"})
        self.write("contracts.json", contracts)
        p, report = self.validate()
        self.assertEqual(p.returncode, 0, report)
        self.assertTrue(any("orphan.event" in w for w in report["warnings"]), report["warnings"])

    @unittest.skipIf(os.name == "nt", "symlinks need privileges on Windows")
    def test_report_is_never_written_through_a_symlink(self):
        victim = self.root / "victim.txt"
        victim.write_text("ORIGINAL")
        (self.dir / "validation-report.json").unlink(missing_ok=True)
        (self.dir / "validation-report.json").symlink_to(victim)
        p = self.run_validator("--write-report")
        self.assertEqual(victim.read_text(), "ORIGINAL")
        self.assertFalse((self.dir / "validation-report.json").is_symlink())
        self.assertEqual(json.loads((self.dir / "validation-report.json").read_text())["feature_id"], self.feature)
        self.assertEqual(p.returncode, 0)

    @unittest.skipIf(os.name == "nt", "symlinks need privileges on Windows")
    def test_symlinked_work_directory_is_rejected(self):
        outside = Path(tempfile.mkdtemp())
        try:
            shutil.move(str(self.root / ".work"), str(outside / ".work"))
            (self.root / ".work").symlink_to(outside / ".work")
            p = self.run_validator("--write-report")
            self.assertNotEqual(p.returncode, 0)
            self.assertIn("symlink", p.stderr)
        finally:
            shutil.rmtree(outside, ignore_errors=True)


class ExistingAppRegressions(Base):
    feature = EXISTING

    def set_claim(self, classification, rationale, sources=None):
        evidence = self.read("evidence.json")
        claim = evidence["claims"][0]
        claim["classification"] = classification
        claim["rationale"] = rationale
        if sources is not None:
            claim["sources"] = sources
        self.write("evidence.json", evidence)

    def test_q1_does_not_match_q12(self):
        self.set_claim("user-confirmed", "Confirmed by the owner in Q-1", [])
        with (self.dir / "DECISIONS.md").open("a") as fh:
            fh.write("\n| Q-12 | Something else | yes |\n")
        _, report = self.validate()
        self.assertTrue(any("user-confirmed" in e for e in report["errors"]), report["errors"])

    def test_all_cited_decisions_must_exist(self):
        self.set_claim("user-confirmed", "Confirmed in Q-2 and Q-3", [])
        with (self.dir / "DECISIONS.md").open("a") as fh:
            fh.write("\n| Q-2 | Scope | current behaviour |\n")
        _, report = self.validate()
        self.assertTrue(any("user-confirmed" in e for e in report["errors"]), report["errors"])

    def test_existing_decision_is_accepted_in_both_spellings(self):
        for log_id in ("Q-2", "Q2"):
            with self.subTest(log_id=log_id):
                self.set_claim("user-confirmed", "Confirmed in Q-2", [])
                (self.dir / "DECISIONS.md").write_text(f"# Decisions\n\n| {log_id} | Scope | current behaviour |\n")
                p, report = self.validate()
                self.assertEqual(p.returncode, 0, report)

    def test_ready_observed_source_requires_sha256(self):
        evidence = self.read("evidence.json")
        del evidence["claims"][0]["sources"][0]["sha256"]
        self.write("evidence.json", evidence)
        _, report = self.validate()
        self.assertTrue(any("requires sha256" in e for e in report["errors"]), report["errors"])

    def test_draft_observed_source_may_omit_sha256(self):
        manifest = self.read("manifest.json")
        manifest["status"] = "draft"
        self.write("manifest.json", manifest)
        evidence = self.read("evidence.json")
        del evidence["claims"][0]["sources"][0]["sha256"]
        self.write("evidence.json", evidence)
        p, report = self.validate()
        self.assertEqual(p.returncode, 0, report)

    def test_documentation_alone_is_not_observed_evidence(self):
        (self.root / "README.md").write_text("# App\n\nPress d to delete.\n")
        self.set_claim("observed", "README says so", [{"path": "README.md", "line_start": 1, "line_end": 3}])
        _, report = self.validate()
        self.assertTrue(any("documentation alone" in e for e in report["errors"]), report["errors"])

    def test_generated_artifacts_cannot_be_cited(self):
        self.set_claim("observed", "self reference",
                       [{"path": f".work/flow-spec/{EXISTING}/CONTRACTS.md", "line_start": 1, "line_end": 1}])
        _, report = self.validate()
        self.assertTrue(any("cannot be cited" in e for e in report["errors"]), report["errors"])

    def test_invalid_source_kind_is_rejected(self):
        evidence = self.read("evidence.json")
        evidence["claims"][0]["sources"][0]["kind"] = "vibes"
        self.write("evidence.json", evidence)
        _, report = self.validate()
        self.assertTrue(any("invalid source kind" in e for e in report["errors"]), report["errors"])

    def test_lowercase_todo_in_contracts_json_does_not_block_ready(self):
        contracts = self.read("contracts.json")
        contracts["events"][0]["description"] += " (moves a task from todo to done)"
        self.write("contracts.json", contracts)
        p, report = self.validate()
        self.assertEqual(p.returncode, 0, report)


class ExtraProvenanceRefs(unittest.TestCase):
    class Collect:
        def __init__(self):
            self.errors, self.warnings = [], []

        def error(self, m):
            self.errors.append(m)

        def warn(self, m):
            self.warnings.append(m)

    def run_case(self, status):
        with tempfile.TemporaryDirectory() as tmp:
            folder = Path(tmp)
            (folder / "evidence.json").write_text(json.dumps({
                "schema_version": "1.0.0", "gaps": [],
                "claims": [{"ref": "events.a", "classification": "inferred", "rationale": "guess", "sources": []}],
            }))
            contracts = {"events": [{"id": "a"}]}
            check = self.Collect()
            loader = lambda path, chk: json.loads(path.read_text())  # noqa: E731
            VE.verify_evidence(folder, folder, contracts, {"status": status}, check, loader, ["screens.main"])
            return check

    def test_missing_screen_claim_is_a_warning_while_draft(self):
        check = self.run_case("draft")
        self.assertTrue(any("screens.main" in w for w in check.warnings))
        self.assertFalse(any("screens.main" in e for e in check.errors))

    def test_missing_screen_claim_is_an_error_when_ready(self):
        check = self.run_case("ready")
        self.assertTrue(any("screens.main" in e for e in check.errors))


class PlaceholderHelper(unittest.TestCase):
    def test_cases(self):
        hit = VE.placeholder_hits
        self.assertFalse(hit("SPEC.md", "Statuses: todo, doing"))
        self.assertFalse(hit("SPEC.md", "See `// TODO` in the source"))
        self.assertFalse(hit("SPEC.md", "```\nTODO\n```"))
        self.assertTrue(hit("SPEC.md", "TODO: fill in"))
        self.assertTrue(hit("SPEC.md", "[TBD]"))
        self.assertTrue(hit("contracts.json", '{"x": "{{name}}"}'))
        self.assertFalse(hit("contracts.json", '{"x": "TODO list"}'))


if __name__ == "__main__":
    unittest.main()
