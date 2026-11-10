"""Check native versions before building, then validate the complete release."""
import argparse
import json
import os
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]


def read_value(path, pattern):
    # Read metadata as text; never execute a PKGBUILD to discover its version.
    match = re.search(pattern, path.read_text(encoding="utf-8"), re.MULTILINE)
    if match is None:
        raise ValueError(f"Missing or unsupported version metadata: {path}")
    return match.group(1)


def verify_sources(root, tag=""):
    version = json.loads((root / "mygo.json").read_text(encoding="utf-8"))["version"]
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", version):
        raise ValueError(f"Unsupported native release version: {version}")
    arch = root / "packaging/arch-native/PKGBUILD"
    arch_version = read_value(arch, r"^pkgver=([0-9.]+)\s*$")
    pkgrel = read_value(arch, r"^pkgrel=([1-9][0-9]*)\s*$")
    app_version = read_value(root / "internal/desktop/launch.go", r'^const Version = "([^"]+)"\s*$')
    for source, actual in [("Arch pkgver", arch_version), ("desktop.Version", app_version)]:
        if actual != version:
            raise ValueError(f"{source} is {actual}; mygo.json requires {version}")
    if tag:
        if tag != "v" + version:
            raise ValueError(f"Tag {tag} and application version {version} disagree")
        notes = root / "docs/releases" / f"{tag}.md"
        if not notes.is_file() or not notes.read_text(encoding="utf-8").strip():
            raise ValueError(f"Missing/empty release notes: {notes}")
    return version, pkgrel


def expected_assets(version, pkgrel):
    return [
        f"screenpin-{version}-linux-amd64.deb",
        f"screenpin-{version}-linux-amd64-portable.tar.gz",
        f"screenpin-{version}-windows-amd64-setup.exe",
        f"screenpin-{version}-windows-amd64-portable.zip",
        f"screenpin-{version}-darwin-arm64.dmg",
        f"screenpin-{version}-darwin-amd64.dmg",
        f"screenpin-native-{version}-{pkgrel}-x86_64.pkg.tar.zst",
    ]


def verify_assets(folder, version, pkgrel):
    expected = expected_assets(version, pkgrel)
    for name in expected:
        path = folder / name
        if path.is_symlink() or not path.is_file() or path.stat().st_size <= 1_000_000:
            raise ValueError(f"Missing/empty installer: {name}")
    extras = set(p.name for p in folder.iterdir()) - set(expected)
    if extras:
        raise ValueError(f"Unexpected release files: {sorted(extras)}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("folder", nargs="?", type=Path, help="omit to check source versions only")
    args = parser.parse_args()
    tag = os.environ.get("RELEASE_TAG", "")
    if args.folder is not None and not tag:
        parser.error("RELEASE_TAG is required when validating release assets")
    try:
        version, pkgrel = verify_sources(ROOT, tag)
        print(f"Verified native version {version}, Arch pkgrel {pkgrel}")
        if args.folder is not None:
            verify_assets(args.folder, version, pkgrel)
            print("Verified all seven platform installers/portable archives")
    except (ValueError, OSError, KeyError) as exc:
        print(f"Release validation failed: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
