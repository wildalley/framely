"""Reject mixed old/new OCR bundles before collecting release installers."""
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("package_release", Path(__file__).with_name("package-release.py"))
package = importlib.util.module_from_spec(spec)
spec.loader.exec_module(package)


class PortableInstallerTests(unittest.TestCase):
    def test_changed_sdk_installer_requires_review(self):
        with self.assertRaisesRegex(ValueError, "archive handling changed"):
            package.linux_portable_installer("#!/bin/sh\n")

    @unittest.skipUnless(sys.platform == "linux", "Linux installer")
    def test_extracted_runtime_and_explicit_archive(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "portable app"
            source.mkdir()
            for name in ("screenpin", "screenpin.png", package.DESKTOP_ID, "LICENSE"):
                (source / name).write_text(name, encoding="utf-8")
            (source / "screenpin").chmod(0o755)
            (source / "workers").mkdir()
            (source / "workers/worker").touch()
            script = source / "install.sh"
            # Run the injected bootstrap with the same variables as the SDK.
            # The SDK continues by extracting the selected archive.
            script.write_text(package.linux_portable_installer('''#!/bin/sh
set -eu
name=screenpin
work="$1"
archive="${2:-}"
\tif [ -z "$archive" ]; then
\t\texit 1
\tfi
tar -tzf "$archive"
'''), encoding="utf-8")
            result = subprocess.check_output(["sh", str(script), str(root)], text=True)
            names = result.splitlines()
            self.assertIn("screenpin", names)
            self.assertIn(package.DESKTOP_ID, names)
            self.assertIn("workers/worker", names)
            self.assertNotIn("screenpin.desktop", names)
            # An explicit archive must still work without adjacent binaries.
            (source / "screenpin").unlink()
            explicit = subprocess.check_output(["sh", str(script), str(root), str(root / "portable.tar.gz")], text=True)
            self.assertEqual(result, explicit)


class BundleResourceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.resources = Path(self.temp.name)
        self.worker = self.resources / "screenpin-ocr"
        (self.worker / "licenses").mkdir(parents=True)
        (self.worker / "licenses/LICENSE.jieba").write_text("license", encoding="utf-8")
        self.manifest = {
            "offline": True, "protocol": 2, "rapidocr": "3.9.2", "onnxruntime": "1.24.4",
            "worker_sha256": hashlib.sha256((package.ROOT / "workers/ocr/worker.py").read_bytes()).hexdigest(),
            "dependency_locks": {name: hashlib.sha256((package.ROOT / "workers/ocr" / name).read_bytes()).hexdigest()
                                 for name in ("requirements.lock", "rapidocr.lock")},
        }
        (self.worker / "sbom.cdx.json").write_text(json.dumps({
            "bomFormat": "CycloneDX", "components": [
                {"name": "rapidocr", "version": "3.9.2"},
                {"name": "onnxruntime", "version": "1.24.4"},
            ],
        }), encoding="utf-8")

    def verify(self):
        (self.worker / "manifest.json").write_text(json.dumps(self.manifest), encoding="utf-8")
        package.verify_ocr_resources(self.resources)

    def test_current_resources_pass(self):
        self.verify()

    def test_stale_protocol_lock_or_worker_is_rejected(self):
        for field, value in (("protocol", 1), ("dependency_locks", {}), ("worker_sha256", "old")):
            old = self.manifest[field]
            self.manifest[field] = value
            with self.assertRaises(ValueError):
                self.verify()
            self.manifest[field] = old

    def test_inventory_and_licenses_are_required(self):
        self.manifest["rapidocr"] = "old"
        with self.assertRaisesRegex(ValueError, "inventory disagrees"):
            self.verify()
        self.manifest["rapidocr"] = "3.9.2"
        (self.worker / "licenses/LICENSE.jieba").unlink()
        with self.assertRaisesRegex(ValueError, "license"):
            self.verify()


class MediaResourceTests(unittest.TestCase):
    def test_media_bundle_rejects_missing_or_modified_runtime(self):
        with tempfile.TemporaryDirectory() as directory:
            resources=Path(directory)
            files={}
            for name in ("screenpin-record", "screenpin-ffmpeg"):
                data=(name.encode()*10000)
                (resources/name).write_bytes(data)
                files[name]=hashlib.sha256(data).hexdigest()
            (resources/"media-manifest.json").write_text(json.dumps({"protocol":1,"ffmpeg":"9.0.2","files":files}))
            licenses=resources/"media-licenses";licenses.mkdir()
            for name in ("COPYING.FFmpeg.LGPLv2.1","README.txt","screenpin-media-source.tar.gz","ffmpeg-9.0.2.tar.xz"):
                (licenses/name).write_bytes(b"fixture")
            (licenses/"BUILD.json").write_text(json.dumps({"sha256":hashlib.sha256(b"fixture").hexdigest()}))
            with patch.object(package.subprocess,"check_output",side_effect=["2\n","ffmpeg version 9.0.2\n"]):
                package.verify_media_resources(resources,"linux")
            (resources/"screenpin-record").write_bytes(b"changed"*20000)
            with self.assertRaisesRegex(ValueError,"hash mismatch"):
                package.verify_media_resources(resources,"linux")


if __name__ == "__main__":
    unittest.main()
