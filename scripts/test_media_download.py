"""Exercise source download failures without network access or native tools."""
from contextlib import ExitStack
import hashlib
import http.client
import importlib.util
import io
from pathlib import Path
import tarfile
import tempfile
import unittest
import urllib.error
from unittest.mock import Mock, call, patch

spec = importlib.util.spec_from_file_location("build_media", Path(__file__).with_name("build-media.py"))
media = importlib.util.module_from_spec(spec)
spec.loader.exec_module(media)


class MediaDownloadTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.work = Path(temp.name)
        stack = ExitStack()
        self.addCleanup(stack.close)
        stack.enter_context(patch.object(media, "WORK", self.work))
        self.sleep = stack.enter_context(patch.object(media.time, "sleep"))
        self.urlopen = stack.enter_context(patch.object(media.urllib.request, "urlopen"))
        content = b"verified source\n"
        buffer = io.BytesIO()
        with tarfile.open(fileobj=buffer, mode="w:gz") as archive:
            entry = tarfile.TarInfo("source/README")
            entry.size = len(content)
            archive.addfile(entry, io.BytesIO(content))
        self.data = buffer.getvalue()
        self.checksum = hashlib.sha256(self.data).hexdigest()
        self.url = "https://example.invalid/source.tar.gz"
        self.archive = self.work / "source.tar.gz"
        self.partial = self.work / "source.tar.gz.part"

    def source(self):
        return media.source(self.url, self.checksum, "source")

    def assert_extracted(self):
        self.assertEqual((self.work / "source/README").read_bytes(), b"verified source\n")
        self.assertEqual(self.archive.read_bytes(), self.data)
        self.assertFalse(self.partial.exists())

    def test_connection_reset_then_success(self):
        self.urlopen.side_effect = [
            urllib.error.URLError(ConnectionResetError(104, "Connection reset by peer")),
            io.BytesIO(self.data),
        ]
        self.assertEqual(self.source(), self.work / "source")
        self.assert_extracted()
        self.assertEqual(self.urlopen.call_count, 2)
        self.sleep.assert_called_once_with(2)

    def test_interrupted_response_is_discarded_before_retry(self):
        for failure in (ConnectionResetError("reset"), TimeoutError("timeout"), http.client.IncompleteRead(b"part")):
            with self.subTest(failure=type(failure).__name__):
                self.archive.unlink(missing_ok=True)
                response = io.BytesIO()
                response.read = Mock(side_effect=[b"partial data", failure])

                def retry(*args, **kwargs):
                    self.assertFalse(self.partial.exists())
                    return response if not response.closed else io.BytesIO(self.data)

                self.urlopen.side_effect = retry
                self.source()
                self.assert_extracted()

    def test_transient_http_errors_are_retried(self):
        for status in (408, 429, 500, 502, 503, 504):
            with self.subTest(status=status):
                self.archive.unlink(missing_ok=True)
                self.urlopen.side_effect = [
                    urllib.error.HTTPError(self.url, status, "temporary", None, None),
                    io.BytesIO(self.data),
                ]
                self.source()
                self.assert_extracted()

    def test_retries_are_bounded_and_preserve_the_error(self):
        error = urllib.error.URLError("unavailable")
        self.urlopen.side_effect = error
        with self.assertRaises(urllib.error.URLError) as raised:
            self.source()
        self.assertIs(raised.exception, error)
        self.assertEqual(self.urlopen.call_count, 4)
        self.assertEqual(self.sleep.call_args_list, [call(2), call(4), call(8)])
        self.assertFalse(self.archive.exists())
        self.assertFalse(self.partial.exists())
        self.assertFalse((self.work / "source").exists())

    def test_permanent_http_error_fails_without_retry(self):
        self.urlopen.side_effect = urllib.error.HTTPError(self.url, 404, "missing", None, None)
        with self.assertRaises(urllib.error.HTTPError):
            self.source()
        self.urlopen.assert_called_once()
        self.sleep.assert_not_called()
        self.assertFalse(self.archive.exists())

    def test_checksum_mismatch_is_not_cached_or_retried(self):
        self.urlopen.return_value = io.BytesIO(b"incorrect source")
        with self.assertRaisesRegex(ValueError, "Source checksum mismatch"):
            self.source()
        self.urlopen.assert_called_once()
        self.sleep.assert_not_called()
        self.assertFalse(self.archive.exists())
        self.assertFalse(self.partial.exists())
        self.assertFalse((self.work / "source").exists())

    def test_valid_cached_source_needs_no_network(self):
        self.archive.write_bytes(self.data)
        self.source()
        self.assert_extracted()
        self.urlopen.assert_not_called()

    def test_invalid_cached_source_is_replaced(self):
        self.archive.write_bytes(b"old partial download")
        self.urlopen.return_value = io.BytesIO(self.data)
        self.source()
        self.assert_extracted()
        self.urlopen.assert_called_once()


if __name__ == "__main__":
    unittest.main()
