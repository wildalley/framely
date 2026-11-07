"""Reject a release if any required native installer is missing or empty."""
import json
import os
from pathlib import Path
import sys

root = Path(__file__).resolve().parents[1]
version = json.loads((root / "mygo.json").read_text())["version"]
assert os.environ["RELEASE_TAG"] == "v" + version, "Tag and application version disagree"
folder = Path(sys.argv[1])
expected = [
    f"screenpin-{version}-linux-amd64.deb",
    f"screenpin-{version}-linux-amd64-portable.tar.gz",
    f"screenpin-{version}-windows-amd64-setup.exe",
    f"screenpin-{version}-windows-amd64-portable.zip",
    f"screenpin-{version}-darwin-arm64.dmg",
    f"screenpin-{version}-darwin-amd64.dmg",
    f"screenpin-native-{version}-1-x86_64.pkg.tar.zst",
]
for name in expected:
    path = folder / name
    assert path.is_file() and path.stat().st_size > 1_000_000, f"Missing/empty installer: {name}"
extras = set(p.name for p in folder.iterdir()) - set(expected)
assert not extras, f"Unexpected release files: {extras}"
print("Verified all seven platform installers/portable archives")
