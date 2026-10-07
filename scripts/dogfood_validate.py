#!/usr/bin/env python3
"""Validate strict JSON contracts emitted by live dogfood commands.

Also hosts the harness's small JSON helpers as subcommands, so the bash
harness never embeds Python programs and every JSON read has one tested home.
"""

import json
import os
import re
import sys
from datetime import datetime, timezone
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
# Go writes RFC 3339 timestamps with nanoseconds and a "Z" suffix; Python before
# 3.11 (still the macOS system python3) accepts neither, so both are normalized.
FRACTION_BEYOND_MICROSECONDS = re.compile(r"(\.\d{6})\d+")
UTC_SUFFIX = "Z"
UTC_OFFSET = "+00:00"
SECONDS_PER_HOUR = 3600
# Credential tokens arrive through these variables, never argv, because process
# argument lists are world-readable on shared hosts.
CREDENTIAL_TOKEN_ENV = {
    "server_provider_token": "PROVIDER_TOKEN",
    "tailscale_token": "TAILSCALE_TOKEN",
    "cloudflare_token": "CLOUDFLARE_TOKEN",
}
PRIVATE_FILE_MODE = 0o600


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


def state_field(path, field):
    """Print one dotted field of CLI server state; absent state prints nothing.

    List items are addressed by index (for example tailscale.tags.0), matching
    how the harness reads the first recorded device tag.
    """
    try:
        with Path(path).open(encoding="utf-8") as stream:
            value = json.load(stream)
    except (OSError, ValueError):
        return
    for part in field.split("."):
        if isinstance(value, list) and part.isdigit() and int(part) < len(value):
            value = value[int(part)]
        elif isinstance(value, dict):
            value = value.get(part)
        else:
            value = None
        if value is None:
            return
    print(value)


def namespace_servers(path, namespace):
    """Print servers in already validated discover output owned by the namespace.

    Discover lists every serverpro server at the provider; leftover checks
    must only judge the dogfood namespace.
    """
    with Path(path).open(encoding="utf-8") as stream:
        for candidate in json.load(stream):
            if candidate.get("namespace") == namespace:
                print(candidate["server"])


def age_hours(stamp):
    """Print whole hours since a state timestamp; unparsable prints nothing.

    An unknown age must not fail the run, because it only feeds the kept
    server cost warning.
    """
    normalized = FRACTION_BEYOND_MICROSECONDS.sub(r"\1", stamp).replace(
        UTC_SUFFIX, UTC_OFFSET
    )
    try:
        created = datetime.fromisoformat(normalized)
    except ValueError:
        return
    print(int((datetime.now(timezone.utc) - created).total_seconds() // SECONDS_PER_HOUR))


def count_identity_devices(host, tag):
    """Print how many online tailnet devices on stdin claim the host and tag.

    Offline devices, such as a decoy left by an earlier run, do not compete for
    the identity, so only connected ones count.
    """
    devices = json.load(sys.stdin).get("devices", [])
    print(
        sum(
            1
            for device in devices
            if device.get("hostname") == host
            and tag in (device.get("tags") or [])
            and (device.get("connectedToControl") or device.get("online"))
        )
    )


def decoy_key_request(tag, expiry_seconds, description):
    """Print the Tailscale key request for the identity decoy.

    Single-use, ephemeral, and short-lived, so a leaked key cannot enrol
    anything after the scenario and the device disappears on logout.
    """
    print(
        json.dumps(
            {
                "capabilities": {
                    "devices": {
                        "create": {
                            "reusable": False,
                            "ephemeral": True,
                            "preauthorized": True,
                            "tags": [tag],
                        }
                    }
                },
                "expirySeconds": int(expiry_seconds),
                "description": description,
            }
        )
    )


def created_key():
    """Print the id and secret of a Tailscale key-create response on stdin.

    Two lines on stdout keep the secret out of argv and files; a malformed
    response fails without echoing any of its content.
    """
    try:
        created = json.load(sys.stdin)
        key_id, key = created["id"], created["key"]
    except (ValueError, KeyError, TypeError) as error:
        raise ValidationError("unexpected Tailscale key response") from error
    print(key_id)
    print(key)


def write_credentials(path, namespace, server):
    """Write the CLI credential document with tokens taken from the environment.

    The file is created private, so no other user can read it even briefly.
    """
    payload = {
        "namespace": namespace,
        "server": server,
        "server_provider_token": os.environ[CREDENTIAL_TOKEN_ENV["server_provider_token"]],
        "tailscale_token": os.environ[CREDENTIAL_TOKEN_ENV["tailscale_token"]],
        "tailscale_auth_key": "",
        "cloudflare_token": os.environ[CREDENTIAL_TOKEN_ENV["cloudflare_token"]],
    }
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, PRIVATE_FILE_MODE)
    with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
        os.fchmod(stream.fileno(), PRIVATE_FILE_MODE)
        json.dump(payload, stream, indent=2)
        stream.write("\n")


# Harness helper subcommands and their exact argument counts; anything else is
# treated as a validator KIND so the original validator interface is unchanged.
HELPER_COMMANDS = {
    "state-field": (state_field, 2),
    "namespace-servers": (namespace_servers, 2),
    "age-hours": (age_hours, 1),
    "count-identity-devices": (count_identity_devices, 2),
    "decoy-key-request": (decoy_key_request, 3),
    "created-key": (created_key, 0),
    "write-credentials": (write_credentials, 3),
}


def run_helper(name, args):
    """Run one harness helper subcommand as the shell harness boundary."""
    helper, arity = HELPER_COMMANDS[name]
    if len(args) != arity:
        print(f"usage: dogfood_validate.py {name} takes {arity} argument(s)", file=sys.stderr)
        return 2
    try:
        helper(*args)
    except ValidationError as error:
        print(error, file=sys.stderr)
        return 1
    return 0


def main(argv):
    """Run the validator as the shell harness boundary."""
    if len(argv) > 1 and argv[1] in HELPER_COMMANDS:
        return run_helper(argv[1], argv[2:])
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
