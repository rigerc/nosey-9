#!/usr/bin/env python3
"""Regression tests for contracts extracted from existing application source."""
from __future__ import annotations

import json
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SKILL_ROOT = Path(__file__).resolve().parents[1]
SCRIPT = SKILL_ROOT / "scripts" / "validate.py"
FEATURE = "example-existing-app"
SOURCE_REL = Path(".agents/skills/clackbt-flow-spec/tests/fixtures/existing_app.py")


class ExistingAppTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.folder = self.root / ".work/flow-spec" / FEATURE
        shutil.copytree(SKILL_ROOT / "examples" / FEATURE, self.folder)
        self.source = self.root / SOURCE_REL
        self.source.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy(SKILL_ROOT / "tests/fixtures/existing_app.py", self.source)

    def tearDown(self):
        self.temp.cleanup()

    def read(self, name):
        return json.loads((self.folder / name).read_text())

    def write(self, name, doc):
        (self.folder / name).write_text(json.dumps(doc, indent=2) + "\n")

    def validate(self):
        p = subprocess.run([sys.executable, str(SCRIPT), "--repo-root", str(self.root),
                            "--feature", FEATURE, "--write-report"], capture_output=True, text=True)
        return p, json.loads(p.stdout)

    def test_contracts_only_passes_without_layout_or_machine(self):
        self.assertFalse((self.folder / "screens.json").exists())
        self.assertFalse((self.folder / "machine.json").exists())
        proc, report = self.validate()
        self.assertEqual(proc.returncode, 0, report)
        self.assertEqual(report["error_count"], 0)
        self.assertEqual(report["warning_count"], 0)

    def test_missing_provenance_claim_fails(self):
        evidence = self.read("evidence.json")
        evidence["claims"].pop()
        self.write("evidence.json", evidence)
        proc, report = self.validate()
        self.assertEqual(proc.returncode, 1)
        self.assertTrue(any("missing provenance claim" in err for err in report["errors"]))

    def test_changed_source_hash_detects_drift(self):
        self.source.write_text(self.source.read_text() + "\n# Modified\n")
        proc, report = self.validate()
        self.assertEqual(proc.returncode, 1)
        self.assertTrue(any("source drift" in err for err in report["errors"]))

    def test_source_traversal_fails(self):
        evidence = self.read("evidence.json")
        evidence["claims"][0]["sources"][0]["path"] = "../../outside.py"
        self.write("evidence.json", evidence)
        proc, report = self.validate()
        self.assertEqual(proc.returncode, 1)
        self.assertTrue(any("invalid repo-relative source path" in err for err in report["errors"]))

    def test_inferred_claim_prevents_ready(self):
        evidence = self.read("evidence.json")
        evidence["claims"][0]["classification"] = "inferred"
        evidence["claims"][0]["sources"] = []
        self.write("evidence.json", evidence)
        proc, report = self.validate()
        self.assertEqual(proc.returncode, 1)
        self.assertTrue(any("inferred claims prevent ready" in err for err in report["errors"]))

    def test_inferred_claim_is_allowed_in_draft(self):
        manifest = self.read("manifest.json")
        manifest["status"] = "draft"
        self.write("manifest.json", manifest)
        evidence = self.read("evidence.json")
        evidence["claims"][0]["classification"] = "inferred"
        evidence["claims"][0]["sources"] = []
        self.write("evidence.json", evidence)
        proc, report = self.validate()
        self.assertEqual(proc.returncode, 0, report)

    def test_open_gap_prevents_ready(self):
        evidence = self.read("evidence.json")
        evidence["gaps"] = [{"id": "gap-1", "question": "Which plugin screens exist?", "status": "open"}]
        self.write("evidence.json", evidence)
        proc, report = self.validate()
        self.assertEqual(proc.returncode, 1)
        self.assertTrue(any("open gap" in err for err in report["errors"]))

    def test_missing_emitted_event_fails(self):
        contracts = self.read("contracts.json")
        contracts["ui"]["components"][0]["emits"] = ["nonexistent.event"]
        self.write("contracts.json", contracts)
        proc, report = self.validate()
        self.assertEqual(proc.returncode, 1)
        self.assertTrue(any("missing event" in err for err in report["errors"]))

    def test_contract_only_requires_existing_app_mode(self):
        manifest = self.read("manifest.json")
        manifest["source_mode"] = "brief"
        self.write("manifest.json", manifest)
        proc, report = self.validate()
        self.assertEqual(proc.returncode, 1)
        self.assertTrue(any("contracts-only mode requires" in err for err in report["errors"]))


if __name__ == "__main__":
    unittest.main()
