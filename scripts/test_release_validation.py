"""Release regressions run before any platform builds, without native tooling."""
import importlib.util
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).with_name("verify-release-assets.py")
spec = importlib.util.spec_from_file_location("release_validation", SCRIPT)
validation = importlib.util.module_from_spec(spec)
spec.loader.exec_module(validation)


class ReleaseValidationTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.write("mygo.json", '{"version": "0.2.1"}')
        self.write("packaging/arch-native/PKGBUILD", "pkgver=0.2.1\npkgrel=1\n")
        self.write("internal/desktop/launch.go", 'package desktop\nconst Version = "0.2.1"\n')
        self.write("docs/releases/v0.2.1.md", "ScreenPin 0.2.1 release notes\n")

    def write(self, name, content):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")

    def assets(self):
        folder = self.root / "release-assets"
        folder.mkdir()
        for name in validation.expected_assets("0.2.1", "1"):
            with (folder / name).open("wb") as stream:
                stream.truncate(1_000_001)
        return folder

    def test_complete_release(self):
        version, pkgrel = validation.verify_sources(self.root, "v0.2.1")
        validation.verify_assets(self.assets(), version, pkgrel)

    def test_previous_arch_version_is_rejected_before_build(self):
        self.write("packaging/arch-native/PKGBUILD", "pkgver=0.2.0\npkgrel=1\n")
        with self.assertRaisesRegex(ValueError, "Arch pkgver is 0.2.0"):
            validation.verify_sources(self.root)

    def test_previous_binary_version_is_rejected_before_build(self):
        self.write("internal/desktop/launch.go", 'const Version = "0.2.0"\n')
        with self.assertRaisesRegex(ValueError, "desktop.Version is 0.2.0"):
            validation.verify_sources(self.root)

    def test_wrong_tag_and_missing_notes_are_rejected(self):
        with self.assertRaisesRegex(ValueError, "Tag v0.2.0"):
            validation.verify_sources(self.root, "v0.2.0")
        (self.root / "docs/releases/v0.2.1.md").unlink()
        with self.assertRaisesRegex(ValueError, "Missing/empty release notes"):
            validation.verify_sources(self.root, "v0.2.1")
        # Branch builds do not require release notes yet.
        validation.verify_sources(self.root)

    def test_arch_package_revision_is_respected(self):
        self.write("packaging/arch-native/PKGBUILD", "pkgver=0.2.1\npkgrel=2\n")
        version, pkgrel = validation.verify_sources(self.root)
        folder = self.assets()
        (folder / "screenpin-native-0.2.1-1-x86_64.pkg.tar.zst").rename(
            folder / "screenpin-native-0.2.1-2-x86_64.pkg.tar.zst")
        validation.verify_assets(folder, version, pkgrel)

    def test_stale_arch_artifact_cannot_be_published(self):
        folder = self.assets()
        (folder / "screenpin-native-0.2.1-1-x86_64.pkg.tar.zst").rename(
            folder / "screenpin-native-0.2.0-1-x86_64.pkg.tar.zst")
        with self.assertRaisesRegex(ValueError, "Missing/empty installer: screenpin-native-0.2.1"):
            validation.verify_assets(folder, "0.2.1", "1")

    def test_empty_installer_and_unexpected_file_are_rejected(self):
        folder = self.assets()
        extra = folder / "unrelated.txt"
        extra.write_text("unexpected", encoding="utf-8")
        with self.assertRaisesRegex(ValueError, "Unexpected release files"):
            validation.verify_assets(folder, "0.2.1", "1")
        extra.unlink()
        (folder / "screenpin-0.2.1-windows-amd64-setup.exe").write_bytes(b"")
        with self.assertRaisesRegex(ValueError, "Missing/empty installer"):
            validation.verify_assets(folder, "0.2.1", "1")

    def test_optimized_python_cannot_disable_validation(self):
        env = dict(os.environ, RELEASE_TAG="v0.0.0")
        result = subprocess.run([sys.executable, "-O", str(SCRIPT)], env=env, capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Release validation failed", result.stderr)


if __name__ == "__main__":
    unittest.main()
