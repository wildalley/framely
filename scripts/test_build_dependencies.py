"""Fail preflight if a dependency update bypasses the checked-in hash locks."""
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[1]


class BuildDependencyTests(unittest.TestCase):
    def test_source_pins_and_hashed_locks_agree(self):
        for source, lock in (("requirements.txt", "requirements.lock"), ("rapidocr.in", "rapidocr.lock")):
            text = (ROOT / "workers/ocr" / lock).read_text(encoding="utf-8")
            pins = {pin.lower() for pin in re.findall(r"^([\w.-]+==[^\s;\\]+)", text, re.M)}
            source_pins = {pin.lower() for pin in re.findall(r"^([\w.-]+==[^\s;\\]+)", (ROOT / "workers/ocr" / source).read_text(encoding="utf-8"), re.M)}
            self.assertTrue(source_pins <= pins, f"Regenerate {lock}: {source_pins - pins}")
            self.assertTrue(pins)
            for record in re.split(r"\n(?=\S)", text):
                if record.startswith("#"):
                    continue
                if "==" in record:
                    self.assertRegex(record, r"--hash=sha256:[0-9a-f]{64}\b")
            self.assertNotRegex(text, r"^opencv-python==", "headless runtime must not install GUI OpenCV")

    def test_external_workflow_actions_use_commit_ids(self):
        for path in (ROOT / ".github/workflows").glob("*.yml"):
            for action in re.findall(r"\buses:\s+([^\s#]+)", path.read_text(encoding="utf-8")):
                if action.startswith("./"):
                    continue
                self.assertRegex(action, r"^[\w./-]+@[0-9a-f]{40}$", str(path))


if __name__ == "__main__":
    unittest.main()
