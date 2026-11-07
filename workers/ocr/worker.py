"""One image in (PNG on stdin), one UTF-8 JSON result out. No runtime downloads."""
import contextlib
import json
import os
import sys
from pathlib import Path

MAX_PIXELS = 32_000_000
TILE = 1536
OVERLAP = 192


def origins(length):
    if length <= TILE:
        return [0]
    result = list(range(0, length - TILE + 1, TILE - OVERLAP))
    if result[-1] != length - TILE:
        result.append(length - TILE)
    return result


def recognize(data):
    import io
    from PIL import Image
    import rapidocr
    from rapidocr import RapidOCR

    image = Image.open(io.BytesIO(data))
    w, h = image.size
    if w <= 0 or h <= 0 or w * h > MAX_PIXELS:
        raise ValueError("图片尺寸超出 OCR 范围")
    # Composite alpha on white; transparent RGB must not become OCR content.
    rgba = image.convert("RGBA")
    image = Image.new("RGB", (w, h), "white")
    image.paste(rgba, mask=rgba.getchannel("A"))
    del rgba
    models = Path(rapidocr.__file__).resolve().parent / "models"
    names = {
        "Det.model_path": "PP-OCRv6_det_small.onnx",
        "Rec.model_path": "PP-OCRv6_rec_small.onnx",
        "Cls.model_path": "ch_ppocr_mobile_v2.0_cls_mobile.onnx",
    }
    for name in names.values():
        if not (models / name).is_file():
            raise RuntimeError("离线 OCR 模型缺失，请重新安装完整组件：" + name)
    params = {key: str(models / value) for key, value in names.items()}
    params.update({
        "Global.log_level": "error",
        "Global.max_side_len": TILE,
        "Det.limit_side_len": TILE,
        "Det.limit_type": "max",
        "EngineConfig.onnxruntime.intra_op_num_threads": min(4, os.cpu_count() or 2),
        "EngineConfig.onnxruntime.inter_op_num_threads": 1,
    })
    engine = RapidOCR(params=params)
    xs, ys = origins(w), origins(h)
    lines = []
    for yi, y in enumerate(ys):
        for xi, x in enumerate(xs):
            tile = image.crop((x, y, min(w, x + TILE), min(h, y + TILE)))
            result = engine(tile)
            if result.boxes is None or result.txts is None:
                continue
            top = y if yi == 0 else (ys[yi - 1] + TILE + y) / 2
            bottom = h if yi == len(ys) - 1 else (y + TILE + ys[yi + 1]) / 2
            # Vertical ownership avoids duplicate rows. Horizontal overlaps
            # are retained so the Go client can join split lines by their text.
            for box, text, score in zip(result.boxes, result.txts, result.scores):
                points = [{"x": min(w, max(0.0, float(p[0]) + x)),
                           "y": min(h, max(0.0, float(p[1]) + y))} for p in box]
                cy = sum(p["y"] for p in points) / 4
                if not top <= cy < bottom:
                    continue
                lines.append({"text": text, "confidence": float(score), "box": points})
                if len(lines) > 20000:
                    raise ValueError("图片包含过多文本行，请缩小识别区域")
    return {"model": "RapidOCR 3.9.2 / PP-OCRv6-small (CPU)", "lines": lines}


def main():
    sys.stdout.reconfigure(encoding="utf-8")
    try:
        data = sys.stdin.buffer.read(128 * 1024 * 1024 + 1)
        if len(data) > 128 * 1024 * 1024:
            raise ValueError("输入图片过大")
        with contextlib.redirect_stdout(sys.stderr):
            result = recognize(data)
    except Exception as exc:
        result = {"error": str(exc), "lines": []}
    print(json.dumps(result, ensure_ascii=False, allow_nan=False))


if __name__ == "__main__":
    main()
