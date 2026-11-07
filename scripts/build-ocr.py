"""Run with the pinned OCR build environment, on the target OS."""
import json
import hashlib
import importlib.metadata
from pathlib import Path
import platform
import subprocess
import sys

root = Path(__file__).resolve().parents[1]
goos = {"Windows": "windows", "Linux": "linux", "Darwin": "darwin"}[platform.system()]
arch = {"AMD64": "amd64", "x86_64": "amd64", "aarch64": "arm64", "arm64": "arm64"}[platform.machine()]
destination = root / "resources" / f"{goos}-{arch}" / "workers"
destination.mkdir(parents=True, exist_ok=True)
subprocess.run([
    sys.executable, "-m", "PyInstaller", "--noconfirm", "--clean", "--onedir",
    "--name", "screenpin-ocr", "--collect-all", "rapidocr", "--collect-all", "onnxruntime",
    "--exclude-module", "tkinter", "--exclude-module", "PyQt5",
    "--distpath", str(destination), "--workpath", str(root / "build" / "ocr-pyinstaller"),
    "--specpath", str(root / "build"), str(root / "workers" / "ocr" / "worker.py"),
], check=True)
(destination / "screenpin-ocr" / "manifest.json").write_text(json.dumps({
    "protocol": 1, "rapidocr": "3.9.2", "onnxruntime": "1.24.4", "models": "PP-OCRv6-small",
    "offline": True,
    "worker_sha256": hashlib.sha256((root / "workers/ocr/worker.py").read_bytes()).hexdigest(),
}, indent=2), encoding="utf-8")
notices = destination / "screenpin-ocr" / "licenses"
notices.mkdir(exist_ok=True)
for distribution in importlib.metadata.distributions():
    name = distribution.metadata["Name"]
    for item in distribution.files or []:
        if item.name.lower().startswith(("license", "copying", "copyright", "notice")) or ".dist-info/licenses/" in str(item):
            source = distribution.locate_file(item)
            if source.is_file():
                target = notices / name / str(item).replace("..", "_")
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(source.read_bytes())
(notices / "README.txt").write_text(
    "ScreenPin OCR uses RapidOCR (Apache-2.0), PaddleOCR PP-OCR models (Apache-2.0), "
    "ONNX Runtime (MIT), OpenCV (Apache-2.0), and the Python runtime and libraries.\n"
    "Dependency license texts are included in this directory and _internal.\n"
    "Model source: https://github.com/PaddlePaddle/PaddleOCR\n"
    "Inference source: https://github.com/RapidAI/RapidOCR\n", encoding="utf-8")
