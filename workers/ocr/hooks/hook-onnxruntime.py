"""Only inference runtime libraries; no transformer training/export tooling."""
from pathlib import Path
import sys

from PyInstaller.utils.hooks import collect_dynamic_libs, copy_metadata
from PyInstaller.depend import bindepend

binaries = collect_dynamic_libs("onnxruntime")
datas = copy_metadata("onnxruntime")

if sys.platform == "linux":
    # The pinned Linux wheel embeds the runtime in its Python extension and
    # also ships a separate C API library. Keep that library if any extension
    # actually links it, so a future wheel layout change remains safe.
    import importlib.util
    package = Path(importlib.util.find_spec("onnxruntime").origin).parent
    extensions = list((package / "capi").glob("*pybind*.so"))
    needs_c_api = not extensions or any(
        name.startswith("libonnxruntime.so")
        for extension in extensions
        for name, _ in bindepend.get_imports(str(extension))
    )
    if not needs_c_api:
        binaries = [(src, dst) for src, dst in binaries if not Path(src).name.startswith("libonnxruntime.so")]

excludedimports = ["onnxruntime.tools", "onnxruntime.transformers", "onnxruntime.quantization"]
