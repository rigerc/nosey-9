#!/usr/bin/env python3
"""Unit tests for the bundled framework-neutral spec validator (stdlib only)."""
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
FEATURE = "example-task-tracker"


class ValidationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.feature_dir = self.root / ".work" / "flow-spec" / FEATURE
        shutil.copytree(SKILL_ROOT / "examples" / FEATURE, self.feature_dir)

    def tearDown(self):
        self.temp.cleanup()

    def validate(self):
        p = subprocess.run([sys.executable, str(SCRIPT), "--repo-root", str(self.root), "--feature", FEATURE, "--write-report"],
                           capture_output=True, text=True)
        return p, json.loads(p.stdout)

    def test_complete_example_passes(self):
        process, report = self.validate()
        self.assertEqual(process.returncode, 0, process.stdout + process.stderr)
        self.assertEqual(report["error_count"], 0)
        self.assertEqual(report["warning_count"], 0)
        self.assertEqual(json.loads((self.feature_dir / "validation-report.json").read_text())["status"], "pass")

    def test_unresolved_ready_question_fails(self):
        path = self.feature_dir / "manifest.json"
        doc = json.loads(path.read_text())
        doc["open_questions"] = ["What is the primary entry screen?"]
        path.write_text(json.dumps(doc))
        process, report = self.validate()
        self.assertEqual(process.returncode, 1)
        self.assertTrue(any("unresolved" in e for e in report["errors"]))

    def test_invalid_transition_reference_fails(self):
        path = self.feature_dir / "machine.json"
        doc = json.loads(path.read_text())
        doc["transitions"][0]["event"] = "nonexistent.event"
        path.write_text(json.dumps(doc))
        process, report = self.validate()
        self.assertEqual(process.returncode, 1)
        self.assertTrue(any("unknown event" in e for e in report["errors"]))

    def test_unknown_node_contract_fails(self):
        path = self.feature_dir / "contracts.json"
        doc = json.loads(path.read_text())
        doc["ui"]["components"][0]["node_id"] = "missing-node"
        path.write_text(json.dumps(doc))
        process, report = self.validate()
        self.assertEqual(process.returncode, 1)
        self.assertTrue(any("unknown screen/node" in e for e in report["errors"]))

    def test_invalid_feature_slug_rejected(self):
        p = subprocess.run([sys.executable, str(SCRIPT), "--repo-root", str(self.root), "--feature", "../escape"],
                           capture_output=True, text=True)
        self.assertNotEqual(p.returncode, 0)
        self.assertIn("kebab-case", p.stderr)


if __name__ == "__main__":
    unittest.main()
