#!/usr/bin/env python3
"""CLI validation tests for cloud backup/restore scripts."""

from __future__ import annotations

import argparse

from test_utils import aws_backup, aws_restore, gcp_backup, gcp_restore


def test_positive_int_accepts_valid_values():
    for mod in (aws_backup, aws_restore, gcp_backup, gcp_restore):
        assert mod._positive_int("1") == 1
        assert mod._positive_int("300") == 300


def test_positive_int_rejects_zero_negative_and_non_numeric():
    for mod in (aws_backup, aws_restore, gcp_backup, gcp_restore):
        for value in ("0", "-1", "abc"):
            try:
                mod._positive_int(value)
            except argparse.ArgumentTypeError as exc:
                assert "positive integer" in str(exc)
            else:
                raise AssertionError(f"{mod.__name__} accepted invalid value {value!r}")
