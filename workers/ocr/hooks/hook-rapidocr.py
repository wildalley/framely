"""Bundle the offline ONNX engine without the other inference backends."""
from PyInstaller.utils.hooks import collect_data_files, copy_metadata

datas = collect_data_files("rapidocr") + copy_metadata("rapidocr")
# RapidOCR lazily imports these entry points and the selected engine.
hiddenimports = ["rapidocr.main", "rapidocr.inference_engine.onnxruntime", "onnxruntime"]
excludedimports = [
    "rapidocr.inference_engine.paddle", "rapidocr.inference_engine.pytorch",
    "rapidocr.inference_engine.openvino", "rapidocr.inference_engine.tensorrt",
    "rapidocr.inference_engine.mnn",
]
