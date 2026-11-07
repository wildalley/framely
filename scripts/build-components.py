"""Build host-native workers; uv and a native C/C++ toolchain are build dependencies."""
from pathlib import Path
import os
import platform
import shutil
import subprocess
import sys

root = Path(__file__).resolve().parents[1]
os.chdir(root)
system = platform.system()
if system not in ("Linux", "Windows", "Darwin"):
    sys.exit("Components currently target Linux, Windows and macOS")
uv = shutil.which("uv")
if uv is None:
    sys.exit("Install uv to build the isolated Python 3.12 OCR runtime")
venv = root / "build" / "ocr-venv"
python = venv / ("Scripts/python.exe" if system == "Windows" else "bin/python")
if not python.is_file():
    subprocess.run([uv, "venv", "--python", "3.12", str(venv)], check=True)
if subprocess.check_output([str(python), "-c", "import sys; print('.'.join(map(str, sys.version_info[:2])))"], text=True).strip() != "3.12":
    sys.exit("build/ocr-venv must use Python 3.12; recreate this build environment")
subprocess.run([uv, "pip", "uninstall", "--python", str(python), "opencv-python"], check=True)
subprocess.run([uv, "pip", "install", "--python", str(python), "-r", "workers/ocr/requirements.txt"], check=True)
subprocess.run([uv, "pip", "install", "--python", str(python), "--no-deps", "rapidocr==3.9.2"], check=True)
subprocess.run([str(python), "scripts/build-ocr.py"], check=True)
if system == "Windows":
    subprocess.run(["powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", "scripts/build-capture.ps1"], check=True)
elif system == "Linux":
    subprocess.run(["bash", "scripts/build-capture.sh"], check=True)
else:
    print("macOS: offline OCR bundled; scrolling capture is not yet supported")
