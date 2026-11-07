"""Regression tests for inline CLI Python structural validation."""
import copy
import json
import sys
import unittest
from pathlib import Path

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'scripts'))
from validate import validate_folder  # noqa: E402

class TestInlineCLI(unittest.TestCase):
    def setUp(self):
        self.folder=ROOT/'examples/example-inline-cli'
        self.original={n:json.loads((self.folder/n).read_text()) for n in ['screens.json','machine.json']}

    def test_sample_is_valid(self):
        result=validate_folder(self.folder,'example-inline-cli',ROOT)
        self.assertEqual(result.errors,[],result.errors)

    def test_terminal_requires_process_exit_code(self):
        # A fresh copy is required because this validator reads from files.
        import tempfile
        from shutil import copytree
        with tempfile.TemporaryDirectory() as root:
            target=Path(root)/'.work/flow-spec/example-inline-cli'
            target.parent.mkdir(parents=True)
            copytree(self.folder,target)
            machine=copy.deepcopy(self.original['machine.json'])
            del next(x for x in machine['states'] if x['id']=='deploy.failed')['exit_code']
            (target/'machine.json').write_text(json.dumps(machine))
            r=validate_folder(target,'example-inline-cli',Path(root))
            self.assertTrue(any('requires exit_code' in e for e in r.errors),r.errors)

if __name__=='__main__': unittest.main()
