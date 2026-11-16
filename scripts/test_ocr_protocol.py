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
    def marian(self, direction="zh-en"):
        model = worker.Marian.__new__(worker.Marian)
        model.direction = direction
        model.eos, model.unk = -1, -2
        model.source = types.SimpleNamespace(encode=lambda text, out_type: text.split())
        model.vocab = {str(i): i for i in range(2000)}
        model.vocab[">>cmn_Hans<<"] = -3
        return model

    def test_long_lines_keep_all_tokens_in_bounded_chunks(self):
        for direction in worker.TRANSLATE_DIRECTIONS:
            model = self.marian(direction)
            chunks = model.encode(" ".join(map(str, range(1200))))
            prefix = 1 if direction == "en-zh" else 0
            self.assertTrue(all(len(chunk) <= worker.MAX_TOKENS and chunk[-1] == model.eos for chunk in chunks))
            if prefix:
                self.assertTrue(all(chunk[0] == -3 for chunk in chunks))
            self.assertEqual([token for chunk in chunks for token in chunk[prefix:-1]], list(range(1200)))

    def test_chunked_translation_restores_line_and_chunk_order(self):
        model = self.marian()
        model.batch = lambda rows: [",".join(map(str, row[:-1])) for row in rows]
        source = " ".join(map(str, range(1200)))
        result = model.translate([source, "", "7 8", "  "], batch=2)
        self.assertEqual([int(token) for chunk in result[0].split() for token in chunk.split(",")], list(range(1200)))
        self.assertEqual(result[1:], ["", "7,8", ""])

    def test_chunking_prefers_sentence_boundaries_without_losing_tokens(self):
        model = self.marian()
        model.vocab["."] = 2000
        pieces = list(map(str, range(400))) + ["."] + list(map(str, range(400, 900)))
        chunks = model.encode(" ".join(pieces))
        self.assertEqual(len(chunks[0]), 402)
        self.assertEqual([token for chunk in chunks for token in chunk[:-1]], list(range(400)) + [2000] + list(range(400, 900)))

    def test_chinese_chunks_join_without_inventing_spaces(self):
        model = self.marian("en-zh")
        model.batch = lambda rows: ["译文" for row in rows]
        result = model.translate([" ".join(map(str, range(600)))])
        self.assertEqual(result, ["译文译文"])

    @unittest.skipUnless(importlib.util.find_spec("numpy"), "decoder check needs the OCR build environment")
    def test_decoder_reports_unfinished_output_instead_of_truncating(self):
        import numpy as np
        model = self.marian()
        model.np, model.eos, model.pad = np, 2, 0
        model.encoder = types.SimpleNamespace(run=lambda *args: [np.zeros((1, 1, 1))])
        model.decoder = types.SimpleNamespace(run=lambda _, feeds: [np.array([[[0., 1., 0.]]])])
        with self.assertRaisesRegex(ValueError, "译文超过模型长度"):
            model.batch([[1, model.eos]])

    @unittest.skipUnless(importlib.util.find_spec("numpy"), "decoder check needs the OCR build environment")
    def test_decoder_stops_when_all_rows_reach_eos(self):
        import numpy as np
        model = self.marian()
        model.np, model.eos, model.pad = np, 2, 0
        model.encoder = types.SimpleNamespace(run=lambda *args: [np.zeros((1, 1, 1))])
        model.decoder = Mock()
        model.decoder.run.return_value = [np.array([[[0., 0., 1.]]])]
        model.decode = lambda ids: "done"
        self.assertEqual(model.batch([[1, model.eos]]), ["done"])
        model.decoder.run.assert_called_once()

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
