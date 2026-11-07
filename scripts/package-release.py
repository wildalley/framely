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
    app = bundle / "ScreenPin.app"
    binary = app / "Contents/MacOS/ScreenPin" if goos == "darwin" else bundle / ("ScreenPin.exe" if goos == "windows" else "screenpin")
    resources = app / "Contents/Resources/workers" if goos == "darwin" else bundle / "workers"
    actual = subprocess.check_output([str(binary), "--version"], text=True).strip()
    assert actual == f"ScreenPin {version} (mygo)", actual
    manifest = json.loads((resources / "screenpin-ocr/manifest.json").read_text())
    assert manifest["offline"] and manifest["protocol"] == 1
    if goos != "darwin":
        capture = resources / ("screenpin-capture.exe" if goos == "windows" else "screenpin-capture")
        assert capture.is_file(), "Missing native capture worker"
        if goos == "windows":
            json.loads(subprocess.check_output([str(capture), "--list"], text=True))
    else:
        try:
            subprocess.run(["codesign", "--verify", "--deep", "--strict", "--verbose=4", str(app)], check=True)
        except subprocess.CalledProcessError:
            # codesign's generic ENOENT can also refer to a nested resource.
            for item in app.rglob("*"):
                if item.is_symlink() and not item.exists():
                    print(f"Broken bundle link: {item.relative_to(app)} -> {item.readlink()}", flush=True)
            subprocess.run(["codesign", "--display", "--verbose=4", str(app)])
            raise
    # Test the installed resource copy, including code-signing changes on Mac.
    env = dict(os.environ, SCREENPIN_COMPONENTS_DIR=str(resources), SCREENPIN_OCR_INTEGRATION="1")
    subprocess.run(["go", "test", "-count=1", "-run", "TestPackagedOfflineWorker", "-v", "./internal/ocr"], env=env, check=True)
    if args.verify_only:
        return
    out = ROOT / "build/release"
    out.mkdir(parents=True, exist_ok=True)
    prefix = f"screenpin-{version}-{goos}-{arch}"
    if goos == "darwin":
        shutil.copy2(bundle / f"screenpin-{version}-{arch}.dmg", out / f"{prefix}.dmg")
    elif goos == "windows":
        shutil.copy2(bundle / f"screenpin-setup-{version}-{arch}.exe", out / f"{prefix}-setup.exe")
        with zipfile.ZipFile(out / f"{prefix}-portable.zip", "w", zipfile.ZIP_DEFLATED) as archive:
            for item in [bundle / "ScreenPin.exe", *resources.rglob("*")]:
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
            (portable / "README.txt").write_text("ScreenPin " + version + "\nRun ./screenpin, or ./install.sh to install for the current user.\nKeep the workers directory with the executable.\n")
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
            digest = hashlib.sha256(item.read_bytes()).hexdigest()
            print(f"{digest}  {item.name}")


if __name__ == "__main__":
    main()
