"""Offline OCR: bounded RGB requests, streamed JSON replies, one reusable model.

With --translate, it translates text offline with a downloaded OPUS-MT model.
"""
import contextlib
import json
import os
import re
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
    return {"type": "result", "model": "RapidOCR / PP-OCRv6-small (CPU)", "lines": lines}


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


# Offline translation: Marian (OPUS-MT) models exported to ONNX, run with
# ONNX Runtime, tokenized with SentencePiece. One request per process: the
# model loads in well under a second and nothing stays resident.
TRANSLATE_DIRECTIONS = {"en-zh", "zh-en"}
# English to Chinese has several Chinese targets; the model card asks for
# this tag to choose Simplified Chinese.
TARGET_TAGS = {"en-zh": ">>cmn_Hans<<"}
MAX_TRANSLATE_LINES = 2000
MAX_TRANSLATE_BYTES = 256 * 1024
MAX_TOKENS = 512
CJK = r"[\u3000-\u303f\u3400-\u9fff\uff00-\uffef]"
CJK_SPACE = re.compile(r"(?<=" + CJK + r")\s+|\s+(?=" + CJK + r")")
CJK_PUNCTUATION = str.maketrans({",": "，", ";": "；", ":": "：", "?": "？", "!": "！"})


def tidy_chinese(text):
    """Chinese is written without spaces; SentencePiece leaves some around
    Chinese characters and punctuation, with ASCII punctuation."""
    return CJK_SPACE.sub("", text).translate(CJK_PUNCTUATION)


def read_translate_request(stream):
    data = stream.read(MAX_TRANSLATE_BYTES + 1)
    if len(data) > MAX_TRANSLATE_BYTES:
        raise ValueError("翻译文字过长")
    request = json.loads(data.decode("utf-8"))
    lines = request.get("lines") if isinstance(request, dict) else None
    if not isinstance(lines, list) or len(lines) > MAX_TRANSLATE_LINES or not all(isinstance(line, str) for line in lines):
        raise ValueError("翻译请求无效")
    return lines


class Marian:
    def __init__(self, model_dir, direction):
        import numpy as np
        import onnxruntime as ort
        import sentencepiece as spm

        self.np, self.direction = np, direction
        model = Path(model_dir)
        self.vocab = json.loads((model / "vocab.json").read_text(encoding="utf-8"))
        self.tokens = {i: token for token, i in self.vocab.items()}
        self.source = spm.SentencePieceProcessor(model_file=str(model / "source.spm"))
        self.target = spm.SentencePieceProcessor(model_file=str(model / "target.spm"))
        self.eos, self.unk, self.pad = self.vocab["</s>"], self.vocab["<unk>"], self.vocab["<pad>"]
        providers = ["CPUExecutionProvider"]
        self.encoder = ort.InferenceSession(str(model / "encoder_model_quantized.onnx"), providers=providers)
        self.decoder = ort.InferenceSession(str(model / "decoder_model_quantized.onnx"), providers=providers)

    def encode(self, text):
        tag = TARGET_TAGS.get(self.direction)
        ids = ([self.vocab[tag]] if tag else []) + [self.vocab.get(p, self.unk) for p in self.source.encode(text, out_type=str)]
        return ids[: MAX_TOKENS - 1] + [self.eos]

    def decode(self, ids):
        text = self.target.decode_pieces([self.tokens[i] for i in ids if i not in (self.eos, self.pad) and i in self.tokens]).strip()
        return tidy_chinese(text) if self.direction.endswith("-zh") else text

    def translate(self, lines, batch=16):
        out = [""] * len(lines)
        todo = [i for i, line in enumerate(lines) if line.strip()]
        for start in range(0, len(todo), batch):
            rows = todo[start:start + batch]
            for i, text in zip(rows, self.batch([lines[i].strip() for i in rows])):
                out[i] = text
        return out

    def batch(self, lines):
        """Greedy decoding of a batch, never choosing <pad>, as the model's
        bad_words_ids asks."""
        np = self.np
        encoded = [self.encode(line) for line in lines]
        width = max(len(ids) for ids in encoded)
        input_ids = np.full((len(encoded), width), self.pad, dtype=np.int64)
        mask = np.zeros((len(encoded), width), dtype=np.int64)
        for row, ids in enumerate(encoded):
            input_ids[row, :len(ids)] = ids
            mask[row, :len(ids)] = 1
        hidden = self.encoder.run(None, {"input_ids": input_ids, "attention_mask": mask})[0]
        tokens = np.full((len(encoded), 1), self.pad, dtype=np.int64)
        done = np.zeros(len(encoded), dtype=bool)
        for _ in range(min(MAX_TOKENS, 2 * width + 16)):
            logits = self.decoder.run(None, {"input_ids": tokens, "encoder_attention_mask": mask, "encoder_hidden_states": hidden})[0]
            step = logits[:, -1, :]
            step[:, self.pad] = -np.inf
            following = np.where(done, self.pad, step.argmax(axis=-1))
            tokens = np.concatenate([tokens, following[:, None]], axis=1)
            done |= following == self.eos
            if done.all():
                break
        return [self.decode(row[1:].tolist()) for row in tokens]


def translate_main(model_dir, direction):
    sys.stdout.reconfigure(encoding="utf-8")
    try:
        if direction not in TRANSLATE_DIRECTIONS:
            raise ValueError("不支持的翻译方向")
        lines = read_translate_request(sys.stdin.buffer)
        with contextlib.redirect_stdout(sys.stderr):
            result = {"lines": Marian(model_dir, direction).translate(lines)}
    except Exception as exc:
        result = {"error": str(exc)}
    print(json.dumps(result, ensure_ascii=False, allow_nan=False))


def main():
    if sys.argv[1:] == ["--server"]:
        serve(sys.stdin.buffer, sys.stdout.buffer)
        return
    if len(sys.argv) == 4 and sys.argv[1] == "--translate":
        translate_main(sys.argv[2], sys.argv[3])
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
