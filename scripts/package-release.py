"""Verify host-native bundles and collect complete installers for release."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import tarfile
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]
DESKTOP_ID = "io.screenpin.desktop.desktop"


def verify_ocr_resources(resources):
    worker = resources / "screenpin-ocr"
    manifest = json.loads((worker / "manifest.json").read_text(encoding="utf-8"))
    if not manifest.get("offline") or manifest.get("protocol") != 2:
        raise ValueError("Offline OCR protocol 2 runtime is required")
    for name in ("requirements.lock", "rapidocr.lock"):
        expected = hashlib.sha256((ROOT / "workers/ocr" / name).read_bytes()).hexdigest()
        if manifest.get("dependency_locks", {}).get(name) != expected:
            raise ValueError(f"OCR runtime was not built from the current {name}")
    expected = hashlib.sha256((ROOT / "workers/ocr/worker.py").read_bytes()).hexdigest()
    if manifest.get("worker_sha256") != expected:
        raise ValueError("OCR runtime contains a stale worker implementation")
    sbom = json.loads((worker / "sbom.cdx.json").read_text(encoding="utf-8"))
    if sbom.get("bomFormat") != "CycloneDX" or not sbom.get("components"):
        raise ValueError("Missing OCR dependency inventory")
    inventory = {item["name"]: item["version"] for item in sbom["components"]}
    for name in ("rapidocr", "onnxruntime"):
        if inventory.get(name) != manifest.get(name):
            raise ValueError(f"OCR inventory disagrees with manifest: {name}")
    if not (worker / "licenses/LICENSE.jieba").is_file():
        raise ValueError("Missing segmentation dictionary license")


def verify_media_resources(resources, goos):
    manifest = json.loads((resources / "media-manifest.json").read_text(encoding="utf-8"))
    if manifest.get("protocol") != 1 or manifest.get("ffmpeg") != "9.0.2":
        raise ValueError("Missing or stale media runtime")
    suffix = ".exe" if goos == "windows" else ""
    for name in ("screenpin-record", "screenpin-ffmpeg"):
        path = resources / (name + suffix)
        if not path.is_file() or path.stat().st_size < 100_000:
            raise ValueError(f"Missing media component: {name}")
        # macOS bundle sealing can rewrite Mach-O signatures. Exercise those
        # actual signed copies below; other targets must remain byte-identical.
        if goos != "darwin" and hashlib.sha256(path.read_bytes()).hexdigest() != manifest.get("files", {}).get(name):
            raise ValueError(f"Media component hash mismatch: {name}")
    licenses = resources / "media-licenses"
    for name in ("COPYING.FFmpeg.LGPLv2.1", "BUILD.json", "README.txt", "ffmpeg-9.0.2.tar.xz", "screenpin-media-source.tar.gz"):
        if not (licenses / name).is_file():
            raise ValueError(f"Missing media license/source: {name}")
    build = json.loads((licenses / "BUILD.json").read_text(encoding="utf-8"))
    if hashlib.sha256((licenses / "ffmpeg-9.0.2.tar.xz").read_bytes()).hexdigest() != build["sha256"]:
        raise ValueError("Media source checksum mismatch")
    if subprocess.check_output([str(resources / ("screenpin-record"+suffix)), "--protocol-version"], text=True, timeout=10).strip() != "2":
        raise ValueError("Media capture protocol mismatch")
    version = subprocess.check_output([str(resources / ("screenpin-ffmpeg"+suffix)), "-version"], text=True, timeout=10)
    if "ffmpeg version 9.0.2" not in version or "--enable-gpl" in version or "--enable-nonfree" in version:
        raise ValueError("Unexpected FFmpeg build or license configuration")


def restore_hidden_resources(source, destination):
    # MyGo 0.7.4 skips dot directories, including PyInstaller's .dylibs.
    # Restore them before sealing the bundle and creating the release image.
    for item in source.rglob("*"):
        if not item.name.startswith("."):
            continue
        target = destination / item.relative_to(source)
        if os.path.lexists(target):
            continue
        target.parent.mkdir(parents=True, exist_ok=True)
        if item.is_symlink():
            target.symlink_to(item.readlink())
        elif item.is_dir():
            shutil.copytree(item, target, symlinks=True)
        else:
            shutil.copy2(item, target)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--verify-only", action="store_true")
    args = parser.parse_args()
    os.chdir(ROOT)
    config = json.loads((ROOT / "mygo.json").read_text())
    version = config["version"]
    goos = {"Linux": "linux", "Windows": "windows", "Darwin": "darwin"}[platform.system()]
    arch = {"AMD64": "amd64", "x86_64": "amd64", "arm64": "arm64", "aarch64": "arm64"}[platform.machine()]
    bundle = ROOT / "build" / "native" / f"{goos}-{arch}"
    app = bundle / "Framely.app"
    binary = app / "Contents/MacOS/Framely" if goos == "darwin" else bundle / ("Framely.exe" if goos == "windows" else "screenpin")
    resources = app / "Contents/Resources/workers" if goos == "darwin" else bundle / "workers"
    actual = subprocess.check_output([str(binary), "--version"], text=True).strip()
    if actual != f"Framely {version} (mygo)":
        raise ValueError(f"Unexpected application version: {actual}")
    verify_ocr_resources(resources)
    if goos != "darwin":
        capture = resources / ("screenpin-capture.exe" if goos == "windows" else "screenpin-capture")
        if not capture.is_file():
            raise ValueError("Missing native capture worker")
        if subprocess.check_output([str(capture), "--protocol-version"], text=True, timeout=10).strip() != "2":
            raise ValueError("Native capture protocol 2 runtime is required")
        if goos == "windows":
            json.loads(subprocess.check_output([str(capture), "--list"], text=True))
    else:
        restore_hidden_resources(ROOT / f"resources/darwin-{arch}/workers", resources)
        subprocess.run(["codesign", "--force", "--sign", "-", str(app)], check=True)
        try:
            subprocess.run(["codesign", "--verify", "--deep", "--strict", "--verbose=4", str(app)], check=True)
        except subprocess.CalledProcessError:
            # codesign's generic ENOENT can also refer to a nested resource.
            for item in app.rglob("*"):
                if item.is_symlink() and not item.exists():
                    print(f"Broken bundle link: {item.relative_to(app)} -> {item.readlink()}", flush=True)
            subprocess.run(["codesign", "--display", "--verbose=4", str(app)])
            raise
    verify_media_resources(resources, goos)
    media_env = dict(os.environ, SCREENPIN_COMPONENTS_DIR=str(resources), SCREENPIN_MEDIA_INTEGRATION="1")
    subprocess.run(["go", "test", "-count=1", "-run", "TestPackagedMediaExports", "-v", "./internal/recording"], env=media_env, check=True)
    # Test the installed resource copy, including code-signing changes on Mac.
    env = dict(os.environ, SCREENPIN_COMPONENTS_DIR=str(resources), SCREENPIN_OCR_INTEGRATION="1")
    subprocess.run(["go", "test", "-count=1", "-run", "TestPackagedOfflineWorker", "-v", "./internal/ocr"], env=env, check=True)
    if args.verify_only:
        return
    out = ROOT / "build/release"
    out.mkdir(parents=True, exist_ok=True)
    prefix = f"framely-{version}-{goos}-{arch}"
    if goos == "darwin":
        # Build from the repaired and verified bundle, never the SDK's earlier copy.
        with tempfile.TemporaryDirectory(prefix="dmg-", dir=ROOT / "build") as tmp:
            image_root = Path(tmp) / "image"
            image_root.mkdir()
            subprocess.run(["ditto", "--noclone", str(app), str(image_root / app.name)], check=True)
            (image_root / "Applications").symlink_to("/Applications")
            dmg = out / f"{prefix}.dmg"
            subprocess.run(["hdiutil", "create", "-ov", "-volname", "Framely", "-srcfolder", str(image_root), "-format", "UDZO", str(dmg)], check=True)
            subprocess.run(["hdiutil", "verify", str(dmg)], check=True)
    elif goos == "windows":
        shutil.copy2(bundle / f"screenpin-setup-{version}-{arch}.exe", out / f"{prefix}-setup.exe")
        with zipfile.ZipFile(out / f"{prefix}-portable.zip", "w", zipfile.ZIP_DEFLATED) as archive:
            for item in [bundle / "Framely.exe", *resources.rglob("*")]:
                if item.is_file():
                    archive.write(item, item.relative_to(bundle))
            archive.write(ROOT / "LICENSE", "LICENSE")
    else:
        # The SDK uses slug.desktop; preserve ScreenPin's historical portal ID.
        with tempfile.TemporaryDirectory(prefix="release-", dir=ROOT / "build") as tmp:
            tmp = Path(tmp)
            portable = tmp / "portable"
            portable.mkdir()
            for name in ["screenpin", "screenpin.png"]:
                shutil.copy2(bundle / name, portable / name)
            shutil.copytree(resources, portable / "workers", symlinks=True)
            shutil.copy2(bundle / "screenpin.desktop", portable / DESKTOP_ID)
            installer = (bundle / "install.sh").read_text().replace("$name.desktop", DESKTOP_ID).replace("${name}.desktop", DESKTOP_ID)
            (portable / "install.sh").write_text(installer)
            (portable / "install.sh").chmod(0o755)
            shutil.copy2(ROOT / "LICENSE", portable / "LICENSE")
            (portable / "README.txt").write_text("Framely (formerly ScreenPin) " + version + "\nRun ./screenpin, or ./install.sh to install for the current user.\nKeep the workers directory with the executable.\n")
            with tarfile.open(out / f"{prefix}-portable.tar.gz", "w:gz") as archive:
                for item in sorted(portable.iterdir()):
                    archive.add(item, arcname=item.name)
            deb = tmp / "deb"
            subprocess.run(["dpkg-deb", "--raw-extract", str(bundle / f"screenpin_{version}_{arch}.deb"), str(deb)], check=True)
            desktop = deb / "usr/share/applications/screenpin.desktop"
            desktop.rename(desktop.with_name(DESKTOP_ID))
            sums = deb / "DEBIAN/md5sums"
            sums.write_text(sums.read_text().replace("usr/share/applications/screenpin.desktop", "usr/share/applications/" + DESKTOP_ID))
            subprocess.run(["dpkg-deb", "--build", "--root-owner-group", str(deb), str(out / f"{prefix}.deb")], check=True)
    for item in sorted(out.iterdir()):
        if item.is_file():
            # Installers are hundreds of MB; hash incrementally instead of
            # retaining another complete archive in the packaging process.
            hasher = hashlib.sha256()
            with item.open("rb") as stream:
                for chunk in iter(lambda: stream.read(1 << 20), b""):
                    hasher.update(chunk)
            digest = hasher.hexdigest()
            print(f"{digest}  {item.name}")
            print(f"Release asset size: {item.name} {item.stat().st_size} bytes")


if __name__ == "__main__":
    main()
