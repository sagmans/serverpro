#!/usr/bin/env python3
"""Validate strict JSON contracts emitted by live dogfood commands."""

import json
import re
import sys
from pathlib import Path

PASSING_STATUSES = frozenset({"pass", "warn"})
DOCTOR_STATUSES = ("pass", "warn", "fail", "skip")
DOCTOR_DETAIL_STATUSES = ("warn", "fail", "skip")
DOCTOR_SUMMARY_FIELDS = ("counts", "status", "report_path")
DOCTOR_COUNT_FIELDS = (*DOCTOR_STATUSES, "total")
INVENTORY_TEXT_FIELDS = ("id", "name", "namespace", "server")
INVENTORY_ID_FIELDS = ("namespace", "server")
INVENTORY_STATES = frozenset({"missing", "partial", "present"})
VALID_ID_PATTERN = re.compile(r"^[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?$")
# Provider power labels after the CLI's own mapping: only "running" becomes
# "on", so DigitalOcean reports "active" and Vultr reports "stopped".
POWER_ON_LABELS = frozenset({"on", "active"})
POWER_OFF_LABELS = frozenset({"off", "stopped"})
IMPORTED_STATUS = "imported"
ARGUMENT_COUNT = 6


class ValidationError(ValueError):
    """Reported when command output cannot prove the expected live operation."""


def validate_output(kind, value, provider="", namespace="", server=""):
    """Validate one parsed command result against its semantic contract."""
    if kind == "diagnostics":
        valid = (
            isinstance(value, list)
            and bool(value)
            and all(
                isinstance(item, dict)
                and item.get("status") in PASSING_STATUSES
                for item in value
            )
        )
        if not valid:
            raise ValidationError(
                "diagnostics must be a non-empty array without failing statuses"
            )
        return

    if kind == "catalog":
        valid = (
            isinstance(value, list)
            and bool(value)
            and all(
                isinstance(item, dict)
                and isinstance(item.get("name"), str)
                and bool(item["name"])
                for item in value
            )
        )
        if not valid:
            raise ValidationError("catalog items must have non-empty names")
        return

    if kind == "list":
        valid = (
            isinstance(value, list)
            and all(
                isinstance(item, dict)
                and item.get("provider") == provider
                and all(
                    isinstance(item.get(field), str) and bool(item[field].strip())
                    for field in INVENTORY_TEXT_FIELDS
                )
                and all(
                    VALID_ID_PATTERN.fullmatch(item[field]) is not None
                    for field in INVENTORY_ID_FIELDS
                )
                and item.get("labels_ok") is True
                and item.get("local_state") in INVENTORY_STATES
                for item in value
            )
        )
        if not valid:
            raise ValidationError(
                "inventory candidates must prove provider ownership and local state"
            )
        return

    if kind == "namespace-created":
        if not isinstance(value, dict) or value.get("status") != "created":
            raise ValidationError("namespace create status mismatch")
        if value.get("namespace") != namespace:
            raise ValidationError("namespace mismatch")
        return

    if kind == "doctor-report":
        if not isinstance(value, dict):
            raise ValidationError("doctor report must be an object")
        results = value.get("results")
        # Summary metadata must not bypass validation through the legacy report path.
        if any(field in value for field in DOCTOR_SUMMARY_FIELDS):
            counts = value.get("counts")
            if (
                value.get("namespace") != namespace
                or value.get("server") != server
                or not isinstance(value.get("report_path"), str)
                or not value["report_path"].strip()
                or not isinstance(counts, dict)
                or any(
                    type(counts.get(field)) is not int or counts[field] < 0
                    for field in DOCTOR_COUNT_FIELDS
                )
            ):
                raise ValidationError("doctor summary identity, report path or counts invalid")
            # Omitted pass details need positive, internally consistent check counts.
            if counts["total"] <= 0 or counts["total"] != sum(
                counts[status] for status in DOCTOR_STATUSES
            ):
                raise ValidationError("doctor summary must account for non-empty checks")
            if not isinstance(results, list) or any(
                not isinstance(item, dict)
                or item.get("status") not in DOCTOR_DETAIL_STATUSES
                for item in results
            ):
                raise ValidationError("doctor summary results must contain only warn/fail/skip")
            if any(
                sum(item["status"] == status for item in results) != counts[status]
                for status in DOCTOR_DETAIL_STATUSES
            ):
                raise ValidationError("doctor summary result counts mismatch")
            expected_status = "fail" if counts["fail"] else "warn" if counts["warn"] else "pass"
            if value.get("status") != expected_status or counts["fail"]:
                raise ValidationError("doctor summary status mismatch or failing checks")
            return
        valid = (
            isinstance(results, list)
            and bool(results)
            and all(
                isinstance(item, dict)
                and item.get("status") in PASSING_STATUSES
                for item in results
            )
        )
        if not valid:
            raise ValidationError(
                "doctor report must contain results without failing statuses"
            )
        return

    if kind == "server-status":
        if not isinstance(value, dict):
            raise ValidationError("server status must be an object")
        if value.get("namespace") != namespace:
            raise ValidationError("namespace mismatch")
        if value.get("server") != server:
            raise ValidationError("server mismatch")
        if value.get("provider") != provider:
            raise ValidationError("provider mismatch")
        if not isinstance(value.get("power"), str) or not value["power"]:
            raise ValidationError("server power status missing")
        return

    if kind in ("power-on", "power-off"):
        # Power commands and status share one row; a settled state, not an
        # in-between label such as "stopping", proves the operation finished.
        validate_output("server-status", value, provider, namespace, server)
        expected = POWER_ON_LABELS if kind == "power-on" else POWER_OFF_LABELS
        if value["power"] not in expected:
            raise ValidationError(f"power {value['power']!r} is not settled {kind}")
        return

    if kind == "import-complete":
        # Import exits zero even when a row failed, so success needs exactly one
        # imported row for the requested server.
        rows = (
            [
                item
                for item in value
                if isinstance(item, dict)
                and item.get("namespace") == namespace
                and item.get("server") == server
                and item.get("provider") == provider
            ]
            if isinstance(value, list)
            else []
        )
        if len(rows) != 1 or rows[0].get("status") != IMPORTED_STATUS:
            raise ValidationError("import must report exactly one imported row for the server")
        return

    if kind == "bootstrap-complete":
        if (
            not isinstance(value, dict)
            or value.get("status") != "complete"
            or value.get("action") != "bootstrap"
        ):
            raise ValidationError("bootstrap completion mismatch")
        if value.get("namespace") != namespace:
            raise ValidationError("namespace mismatch")
        if value.get("server") != server:
            raise ValidationError("server mismatch")
        return

    if kind == "delete-complete":
        if (
            not isinstance(value, dict)
            or value.get("status") != "complete"
            or value.get("action") != "delete"
        ):
            raise ValidationError("delete completion mismatch")
        if value.get("namespace") != namespace:
            raise ValidationError("namespace mismatch")
        if value.get("server") != server:
            raise ValidationError("server mismatch")
        if value.get("provider") != provider:
            raise ValidationError("provider mismatch")
        return

    raise ValidationError(f"unknown output validator: {kind}")


def validate_file(kind, path, provider="", namespace="", server=""):
    """Load one complete JSON document before applying semantic validation."""
    try:
        with Path(path).open(encoding="utf-8") as stream:
            value = json.load(stream)
    except (OSError, json.JSONDecodeError) as error:
        raise ValidationError(f"invalid JSON output: {error}") from error
    validate_output(kind, value, provider, namespace, server)


def main(argv):
    """Run the validator as the shell harness boundary."""
    if len(argv) != ARGUMENT_COUNT:
        print(
            "usage: dogfood_validate.py KIND PATH PROVIDER NAMESPACE SERVER",
            file=sys.stderr,
        )
        return 2
    try:
        validate_file(*argv[1:])
    except ValidationError as error:
        print(error, file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
