"""Offline OCR: bounded RGB requests, streamed JSON replies, one reusable model."""
import contextlib
import json
import os
import sys
import struct
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


def load_engine():
    import rapidocr
    from rapidocr import RapidOCR

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
    return RapidOCR(params=params)


def recognize(image, engine, progress=None):
    w, h = image.size
    xs, ys = origins(w), origins(h)
    lines = []
    for yi, y in enumerate(ys):
        for xi, x in enumerate(xs):
            tile = image.crop((x, y, min(w, x + TILE), min(h, y + TILE)))
            result = engine(tile)
            batch = []
            boxes = [] if result.boxes is None or result.txts is None else zip(result.boxes, result.txts, result.scores)
            top = y if yi == 0 else (ys[yi - 1] + TILE + y) / 2
            bottom = h if yi == len(ys) - 1 else (y + TILE + ys[yi + 1]) / 2
            # Vertical ownership avoids duplicate rows. Horizontal overlaps
            # are retained so the Go client can join split lines by their text.
            for box, text, score in boxes:
                points = [{"x": min(w, max(0.0, float(p[0]) + x)),
                           "y": min(h, max(0.0, float(p[1]) + y))} for p in box]
                cy = sum(p["y"] for p in points) / 4
                if not top <= cy < bottom:
                    continue
                batch.append({"text": text, "confidence": float(score), "box": points, "tile": yi * len(xs) + xi + 1})
                lines.append(batch[-1])
                if len(lines) > 20000:
                    raise ValueError("图片包含过多文本行，请缩小识别区域")
            if progress:
                progress({"type": "progress", "completed": yi * len(xs) + xi + 1, "total": len(xs) * len(ys), "lines": batch})
    return {"type": "result", "model": "RapidOCR 3.9.2 / PP-OCRv6-small (CPU)", "lines": lines}


def read_exact(stream, size):
    data = bytearray(size)
    view = memoryview(data)
    offset = 0
    while offset < size:
        n = stream.readinto(view[offset:])
        if not n:
            raise EOFError("OCR 输入被截断")
        offset += n
    return data


def read_request(stream):
    first = stream.read(1)
    if not first:
        return None
    header = first + read_exact(stream, 15)
    magic, w, h, size = struct.unpack("<4sIII", header)
    if magic != b"SPO2" or not 0 < w <= 32768 or not 0 < h <= 32768 or w * h > MAX_PIXELS or size != w * h * 3:
        raise ValueError("OCR 输入尺寸或协议无效")
    return w, h, read_exact(stream, size)


def send(output, message):
    data = json.dumps(message, ensure_ascii=False, allow_nan=False).encode("utf-8")
    if len(data) > 16 * 1024 * 1024:
        raise ValueError("OCR 结果过大")
    output.write(struct.pack("<I", len(data)))
    output.write(data)
    output.flush()


def serve(stream, output):
    engine = None
    while True:
        try:
            request = read_request(stream)
            if request is None:
                return
            w, h, pixels = request
            with contextlib.redirect_stdout(sys.stderr):
                from PIL import Image
                image = Image.frombytes("RGB", (w, h), pixels)
                del request, pixels
                if engine is None:
                    engine = load_engine()
                result = recognize(image, engine, lambda msg: send(output, msg))
                image.close()
                del image
            send(output, result)
            del result
        except Exception as exc:
            send(output, {"type": "result", "error": str(exc), "lines": []})
            return  # A failed request cannot poison the next frame boundary.


def main():
    if sys.argv[1:] == ["--server"]:
        serve(sys.stdin.buffer, sys.stdout.buffer)
        return
    # Keep the single-shot PNG interface for component diagnostics.
    import io
    from PIL import Image
    sys.stdout.reconfigure(encoding="utf-8")
    try:
        data = sys.stdin.buffer.read(128 * 1024 * 1024 + 1)
        if len(data) > 128 * 1024 * 1024:
            raise ValueError("输入图片过大")
        with contextlib.redirect_stdout(sys.stderr):
            source = Image.open(io.BytesIO(data))
            w, h = source.size
            if w <= 0 or h <= 0 or w * h > MAX_PIXELS:
                raise ValueError("图片尺寸超出 OCR 范围")
            rgba = source.convert("RGBA")
            image = Image.new("RGB", (w, h), "white")
            image.paste(rgba, mask=rgba.getchannel("A"))
            result = recognize(image, load_engine())
    except Exception as exc:
        result = {"error": str(exc), "lines": []}
    print(json.dumps(result, ensure_ascii=False, allow_nan=False))


if __name__ == "__main__":
    main()
