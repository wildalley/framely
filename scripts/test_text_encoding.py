"""Release scripts run on Windows, whose default text encoding is not UTF-8.

mygo.json and desktop entries contain Chinese text, so every text read must
name its encoding or Windows packaging fails with UnicodeDecodeError.
"""
from pathlib import Path
import re
import unittest

SCRIPTS = Path(__file__).resolve().parent


class TextEncodingTest(unittest.TestCase):
    def test_text_reads_name_utf8(self):
        offenders = []
        for path in sorted(SCRIPTS.glob("*.py")):
            for number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
                if re.search(r"\.read_text\(\s*\)", line):
                    offenders.append(f"{path.name}:{number}")
        self.assertEqual(offenders, [], "read_text() without encoding")


if __name__ == "__main__":
    unittest.main()
