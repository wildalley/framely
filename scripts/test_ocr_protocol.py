"""Bounded OCR transport and model reuse without native/model dependencies."""
import importlib.util
import io
import json
from pathlib import Path
import struct
import types
import unittest
from unittest.mock import Mock, patch

spec = importlib.util.spec_from_file_location("ocr_worker", Path(__file__).resolve().parents[1] / "workers/ocr/worker.py")
worker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(worker)


def frame(w=2, h=1, pixels=b"\xff" * 6):
    return struct.pack("<4sIII", b"SPO2", w, h, len(pixels)) + pixels


def messages(data):
    stream = io.BytesIO(data)
    result = []
    while header := stream.read(4):
        size, = struct.unpack("<I", header)
        result.append(json.loads(stream.read(size)))
    return result


class ProtocolTests(unittest.TestCase):
    def test_exact_frame_boundaries(self):
        stream = io.BytesIO(frame() + frame())
        for _ in range(2):
            self.assertEqual(worker.read_request(stream), (2, 1, b"\xff" * 6))
        self.assertIsNone(worker.read_request(stream))

    def test_reject_dimensions_before_allocating_payload(self):
        for header in [(b"SPO1", 2, 1, 6), (b"SPO2", 0, 1, 0), (b"SPO2", 32769, 1, 98307), (b"SPO2", 8000, 8000, 192000000), (b"SPO2", 2, 1, 7)]:
            with self.subTest(header=header), self.assertRaises(ValueError):
                worker.read_request(io.BytesIO(struct.pack("<4sIII", *header)))

    def test_truncated_frames_are_fatal(self):
        for data in [frame()[:4], frame()[:-1]]:
            with self.assertRaises(EOFError):
                worker.read_request(io.BytesIO(data))
            output = io.BytesIO()
            worker.serve(io.BytesIO(data), output)
            self.assertTrue(messages(output.getvalue())[0]["error"])

    def test_two_requests_reuse_engine_and_release_images(self):
        images = [Mock(), Mock()]
        image_api = Mock()
        image_api.frombytes.side_effect = images
        engine = object()
        output = io.BytesIO()

        def recognize(image, used_engine, progress):
            self.assertIs(used_engine, engine)
            progress({"type": "progress", "completed": 1, "total": 1, "lines": []})
            return {"type": "result", "lines": []}

        with patch.dict("sys.modules", {"PIL": types.SimpleNamespace(Image=image_api)}), patch.object(worker, "load_engine", return_value=engine) as load, patch.object(worker, "recognize", side_effect=recognize):
            worker.serve(io.BytesIO(frame() + frame()), output)
            load.assert_called_once()
        self.assertEqual([m["type"] for m in messages(output.getvalue())], ["progress", "result", "progress", "result"])
        for image in images:
            image.close.assert_called_once()

    def test_invalid_numbers_cannot_cross_protocol(self):
        with self.assertRaises(ValueError):
            worker.send(io.BytesIO(), {"confidence": float("nan")})


class TranslateTests(unittest.TestCase):
    def test_chinese_output_loses_sentencepiece_spaces(self):
        self.assertEqual(worker.tidy_chinese("框架 支持本地识别、 选字和全文编辑 。"), "框架支持本地识别、选字和全文编辑。")
        self.assertEqual(worker.tidy_chinese("单击 Save 以保留, 好吗?"), "单击Save以保留，好吗？")
        self.assertEqual(worker.tidy_chinese("Windows 10 / Linux"), "Windows 10 / Linux")

    def test_requests_are_bounded_lists_of_strings(self):
        self.assertEqual(worker.read_translate_request(io.BytesIO('{"lines": ["a", "", "中文"]}'.encode())), ["a", "", "中文"])
        for data in [b"[]", b'{"lines": "a"}', b'{"lines": [1]}', json.dumps({"lines": ["a"] * 2001}).encode(),
                     b'{"lines": ["' + b"a" * (256 * 1024) + b'"]}']:
            with self.subTest(data=data[:40]), self.assertRaises(ValueError):
                worker.read_translate_request(io.BytesIO(data))

    def test_english_to_chinese_asks_for_simplified_chinese(self):
        self.assertEqual(worker.TARGET_TAGS, {"en-zh": ">>cmn_Hans<<"})
        self.assertEqual(worker.TRANSLATE_DIRECTIONS, {"en-zh", "zh-en"})


if __name__ == "__main__":
    unittest.main()
