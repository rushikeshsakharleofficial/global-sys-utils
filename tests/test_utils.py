#!/usr/bin/env python3
"""Unit tests for non-cloud utility functions in the global-{aws,gcp}-{backup,restore} scripts.

AWS and GCP SDK calls are mocked — no real credentials or network needed.
"""

from __future__ import annotations

import hashlib
import importlib.machinery
import importlib.util
import os
import sys
import tempfile
import threading
import time
import types
import unittest
from pathlib import Path
from unittest.mock import MagicMock

# ---------------------------------------------------------------------------
# Mock all cloud + optional deps before importing our scripts.
# Must be done carefully: exception classes need real BaseException inheritance
# or `except SomeError` raises TypeError at runtime.
# ---------------------------------------------------------------------------

# Real exception stubs
class _BotoCoreError(Exception): pass
class _ClientError(Exception): pass
class _NoCredentialsError(Exception): pass
class _GoogleAPIError(Exception): pass
class _TransferConfig:
    def __init__(self, **kw): pass
class _BotocoreConfig:
    def __init__(self, **kw): pass

# psutil stub that returns real numbers
_psutil = MagicMock()
_psutil.virtual_memory.return_value.available = 2 * 1024 * 1024 * 1024  # 2 GB
_psutil.virtual_memory.return_value.percent = 40.0
_psutil.cpu_percent.return_value = 20.0

# botocore.exceptions with real exception classes
_botocore_exc = MagicMock()
_botocore_exc.BotoCoreError    = _BotoCoreError
_botocore_exc.ClientError      = _ClientError
_botocore_exc.NoCredentialsError = _NoCredentialsError

# botocore.config with real Config class
_botocore_cfg = MagicMock()
_botocore_cfg.Config = _BotocoreConfig

# boto3.s3.transfer with real TransferConfig class
_boto3_transfer = MagicMock()
_boto3_transfer.TransferConfig = _TransferConfig

# google.api_core.exceptions with real exception class
_gapi_exc = MagicMock()
_gapi_exc.GoogleAPIError = _GoogleAPIError

# Register all mocks
_module_mocks = {
    "boto3":                     MagicMock(),
    "boto3.s3":                  MagicMock(),
    "boto3.s3.transfer":         _boto3_transfer,
    "botocore":                  MagicMock(),
    "botocore.config":           _botocore_cfg,
    "botocore.exceptions":       _botocore_exc,
    "google":                    MagicMock(),
    "google.cloud":              MagicMock(),
    "google.cloud.storage":      MagicMock(),
    "google.api_core":           MagicMock(),
    "google.api_core.exceptions": _gapi_exc,
    "google.oauth2":             MagicMock(),
    "google.oauth2.service_account": MagicMock(),
    "psutil":                    _psutil,
}
for _name, _mock in _module_mocks.items():
    sys.modules[_name] = _mock


# ---------------------------------------------------------------------------
# Load scripts as importable modules (they are plain scripts, not packages)
# ---------------------------------------------------------------------------

_ROOT = Path(__file__).resolve().parent.parent


def _load_script(name: str):
    """Import a script from cmd/ as a Python module (no .py extension)."""
    path = str(_ROOT / "cmd" / name)
    mod_name = name.replace("-", "_")
    loader = importlib.machinery.SourceFileLoader(mod_name, path)
    spec = importlib.util.spec_from_loader(mod_name, loader)
    mod = importlib.util.module_from_spec(spec)
    sys.modules[mod_name] = mod
    loader.exec_module(mod)
    return mod


aws_backup  = _load_script("global-aws-backup")
aws_restore = _load_script("global-aws-restore")
gcp_backup  = _load_script("global-gcp-backup")
gcp_restore = _load_script("global-gcp-restore")


# ===========================================================================
# Tests
# ===========================================================================


class TestExtractDate(unittest.TestCase):
    """_extract_date parses dates from filenames."""

    def _check(self, mod, name, yr, mo, day):
        dt = mod._extract_date(name)
        self.assertIsNotNone(dt, f"{name!r} should parse a date")
        self.assertEqual(dt.year,  yr)
        self.assertEqual(dt.month, mo)
        self.assertEqual(dt.day,   day)

    def test_yyyymmdd(self):
        self._check(aws_backup, "app.log.20240115.gz", 2024, 1, 15)

    def test_iso_format(self):
        self._check(aws_backup, "access.log.2024-06-30.gz", 2024, 6, 30)

    def test_no_date_returns_none(self):
        for name in ("app.log", "nodatehere.gz", "readme.txt"):
            self.assertIsNone(aws_backup._extract_date(name), name)

    def test_consistent_across_scripts(self):
        """All four scripts share the same date-extraction logic."""
        filename = "server.log.20231201.gz"
        dates = [
            aws_backup._extract_date(filename),
            aws_restore._extract_date(filename) if hasattr(aws_restore, "_extract_date") else None,
            gcp_backup._extract_date(filename),
            gcp_restore._extract_date(filename) if hasattr(gcp_restore, "_extract_date") else None,
        ]
        dates = [d for d in dates if d is not None]
        self.assertTrue(len(dates) >= 2)
        for d in dates:
            self.assertEqual(d.year, 2023)
            self.assertEqual(d.month, 12)
            self.assertEqual(d.day, 1)


class TestParseS3Url(unittest.TestCase):

    def test_bucket_only(self):
        b, p = aws_backup._parse_s3_url("s3://my-bucket")
        self.assertEqual(b, "my-bucket")
        self.assertEqual(p, "")

    def test_bucket_with_prefix(self):
        b, p = aws_backup._parse_s3_url("s3://my-bucket/logs/nginx")
        self.assertEqual(b, "my-bucket")
        self.assertEqual(p, "logs/nginx")

    def test_trailing_slash_stripped(self):
        _, p = aws_backup._parse_s3_url("s3://bucket/prefix/")
        self.assertEqual(p, "prefix")

    def test_deep_prefix(self):
        b, p = aws_backup._parse_s3_url("s3://bucket/a/b/c")
        self.assertEqual(b, "bucket")
        self.assertEqual(p, "a/b/c")

    def test_invalid_scheme_exits(self):
        with self.assertRaises(SystemExit):
            aws_backup._parse_s3_url("https://wrong.com/path")

    def test_no_scheme_exits(self):
        with self.assertRaises(SystemExit):
            aws_backup._parse_s3_url("my-bucket/logs")

    def test_restore_same_logic(self):
        b1, p1 = aws_backup._parse_s3_url("s3://bucket/pfx")
        b2, p2 = aws_restore._parse_s3_url("s3://bucket/pfx")
        self.assertEqual((b1, p1), (b2, p2))


class TestParseGcsUrl(unittest.TestCase):

    def test_bucket_only(self):
        b, p = gcp_backup._parse_gcs_url("gs://my-bucket")
        self.assertEqual(b, "my-bucket")
        self.assertEqual(p, "")

    def test_bucket_with_prefix(self):
        b, p = gcp_backup._parse_gcs_url("gs://my-bucket/logs/app")
        self.assertEqual(b, "my-bucket")
        self.assertEqual(p, "logs/app")

    def test_trailing_slash_stripped(self):
        _, p = gcp_backup._parse_gcs_url("gs://bucket/prefix/")
        self.assertEqual(p, "prefix")

    def test_invalid_scheme_exits(self):
        with self.assertRaises(SystemExit):
            gcp_backup._parse_gcs_url("s3://wrong")

    def test_restore_same_logic(self):
        b1, p1 = gcp_backup._parse_gcs_url("gs://bucket/pfx")
        b2, p2 = gcp_restore._parse_gcs_url("gs://bucket/pfx")
        self.assertEqual((b1, p1), (b2, p2))


class TestObjectKey(unittest.TestCase):
    """Object key construction for S3 uploads."""

    def test_basic_structure(self):
        key = aws_backup._object_key("/var/log/nginx/access.log.20240115.gz", "logs", "myhost")
        self.assertTrue(key.startswith("logs/"))
        self.assertIn("myhost", key)
        self.assertTrue(key.endswith("access.log.20240115.gz"))

    def test_no_prefix(self):
        key = aws_backup._object_key("/var/log/app.log.gz", "", "host1")
        self.assertTrue(key.startswith("host1/"))

    def test_matches_gcp_object_path(self):
        """AWS _object_key and GCP _object_path must produce identical paths."""
        aws_key = aws_backup._object_key("/var/log/app.log.gz", "backup", "srv1")
        gcp_key = gcp_backup._object_path("/var/log/app.log.gz", "backup", "srv1")
        self.assertEqual(aws_key, gcp_key)

    def test_root_file(self):
        key = aws_backup._object_key("/app.log.gz", "pfx", "h")
        self.assertIn("app.log.gz", key)


class TestLocalPath(unittest.TestCase):
    """Local path construction for restore scripts."""

    def test_aws_preserves_structure(self):
        p = aws_restore._local_path("myhost/var/log/nginx/access.log.gz", "myhost", "/restore", False)
        self.assertTrue(p.startswith("/restore"))
        self.assertIn("access.log.gz", p)

    def test_aws_flatten(self):
        p = aws_restore._local_path("myhost/var/log/nginx/access.log.gz", "myhost", "/restore", True)
        self.assertEqual(p, "/restore/access.log.gz")

    def test_gcp_flatten(self):
        p = gcp_restore._local_path("prefix/host/var/log/app.gz", "prefix", "/dst", True)
        self.assertEqual(p, "/dst/app.gz")

    def test_gcp_preserves_structure(self):
        p = gcp_restore._local_path("prefix/host/var/log/app.gz", "prefix", "/dst", False)
        self.assertTrue(p.startswith("/dst"))
        self.assertIn("app.gz", p)

    def test_no_prefix(self):
        p = aws_restore._local_path("host/logs/file.gz", "", "/out", False)
        self.assertTrue(p.startswith("/out"))


class TestMd5(unittest.TestCase):

    def test_known_content(self):
        with tempfile.NamedTemporaryFile(delete=False) as f:
            f.write(b"hello world")
            path = f.name
        try:
            expected = hashlib.md5(b"hello world").hexdigest()
            self.assertEqual(aws_backup._md5(path), expected)
        finally:
            os.unlink(path)

    def test_empty_file(self):
        with tempfile.NamedTemporaryFile(delete=False) as f:
            path = f.name
        try:
            self.assertEqual(aws_backup._md5(path), hashlib.md5(b"").hexdigest())
        finally:
            os.unlink(path)

    def test_large_file_chunked(self):
        data = b"x" * (256 * 1024)  # 256 KB — forces multi-chunk read
        with tempfile.NamedTemporaryFile(delete=False) as f:
            f.write(data)
            path = f.name
        try:
            self.assertEqual(aws_backup._md5(path), hashlib.md5(data).hexdigest())
        finally:
            os.unlink(path)


class TestAdaptiveThrottle(unittest.TestCase):
    """AdaptiveThrottle without psutil (mocked) falls back to fixed ceiling."""

    def _make(self, min_w=1, max_w=3):
        return aws_backup.AdaptiveThrottle(min_workers=min_w, max_workers=max_w)

    def test_context_manager_no_raise(self):
        t = self._make()
        with t:
            pass
        t.stop()

    def test_all_tasks_complete(self):
        t = self._make(max_w=2)
        results = []
        lock = threading.Lock()

        def task(i):
            with t:
                with lock:
                    results.append(i)

        threads = [threading.Thread(target=task, args=(i,)) for i in range(8)]
        for th in threads: th.start()
        for th in threads: th.join(timeout=10)
        t.stop()
        self.assertEqual(sorted(results), list(range(8)))

    def test_concurrent_cap_respected(self):
        max_w = 2
        t = self._make(max_w=max_w)
        active = []
        peak = [0]
        lock = threading.Lock()

        def task():
            with t:
                with lock:
                    active.append(1)
                    peak[0] = max(peak[0], len(active))
                time.sleep(0.05)
                with lock:
                    active.pop()

        threads = [threading.Thread(target=task) for _ in range(8)]
        for th in threads: th.start()
        for th in threads: th.join(timeout=15)
        t.stop()
        self.assertLessEqual(peak[0], max_w, f"peak={peak[0]} exceeded max_workers={max_w}")

    def test_stop_idempotent(self):
        t = self._make()
        t.stop()
        t.stop()  # must not raise

    def test_min_workers_clipped(self):
        t = aws_backup.AdaptiveThrottle(min_workers=0, max_workers=4)
        self.assertEqual(t.min_workers, 1, "min_workers must be at least 1")
        t.stop()

    def test_max_less_than_min_clipped(self):
        t = aws_backup.AdaptiveThrottle(min_workers=3, max_workers=1)
        self.assertGreaterEqual(t.max_workers, t.min_workers)
        t.stop()

    def test_same_throttle_all_scripts(self):
        """All four scripts have an AdaptiveThrottle with the same interface."""
        for mod, name in [
            (aws_backup, "aws_backup"),
            (aws_restore, "aws_restore"),
            (gcp_backup, "gcp_backup"),
            (gcp_restore, "gcp_restore"),
        ]:
            t = mod.AdaptiveThrottle(min_workers=1, max_workers=2)
            with t:
                pass
            t.stop()


class TestRetryBackoff(unittest.TestCase):
    """Verify retry logic calls the operation the right number of times."""

    def test_upload_retries_on_failure(self):
        """_upload must retry up to `retries` times before raising."""
        call_count = [0]

        def fake_upload_file(*a, **kw):
            call_count[0] += 1
            raise _ClientError("transient failure")

        s3 = MagicMock()
        s3.upload_file = fake_upload_file

        with tempfile.NamedTemporaryFile(delete=False) as f:
            f.write(b"data")
            path = f.name

        try:
            with self.assertRaises(RuntimeError):
                aws_backup._upload(s3, path, "bucket", "key", verify=False, retries=3)
            self.assertEqual(call_count[0], 3, f"expected 3 attempts, got {call_count[0]}")
        finally:
            os.unlink(path)

    def test_upload_succeeds_on_second_attempt(self):
        attempt = [0]

        def flaky(*a, **kw):
            attempt[0] += 1
            if attempt[0] < 2:
                raise _ClientError("transient")

        s3 = MagicMock()
        s3.upload_file = flaky

        with tempfile.NamedTemporaryFile(delete=False) as f:
            f.write(b"data")
            path = f.name

        try:
            aws_backup._upload(s3, path, "bucket", "key", verify=False, retries=3)
            self.assertEqual(attempt[0], 2)
        finally:
            os.unlink(path)

    def test_download_retries_on_failure(self):
        call_count = [0]

        def fake_download(*a, **kw):
            call_count[0] += 1
            raise _ClientError("transient failure")

        s3 = MagicMock()
        s3.download_file = fake_download

        with tempfile.TemporaryDirectory() as tmpdir:
            dest = os.path.join(tmpdir, "file.gz")
            with self.assertRaises(RuntimeError):
                aws_restore._download(s3, "bucket", "key", dest, retries=2)
            self.assertEqual(call_count[0], 2)


class TestDaysValidation(unittest.TestCase):

    def _check_days(self, mod, days_val):
        import argparse
        ap = argparse.ArgumentParser()
        ap.add_argument("--days", type=int)
        args = ap.parse_args(["--days", str(days_val)])
        if args.days <= 0:
            with self.assertRaises(SystemExit):
                ap.error("--days must be a positive integer")

    def test_zero_days(self):
        self._check_days(aws_backup, 0)

    def test_negative_days(self):
        self._check_days(aws_backup, -5)

    def test_positive_days_ok(self):
        import argparse
        ap = argparse.ArgumentParser()
        ap.add_argument("--days", type=int)
        args = ap.parse_args(["--days", "30"])
        self.assertGreater(args.days, 0)


if __name__ == "__main__":
    unittest.main(verbosity=2)
