#!/usr/bin/env -S uv run --no-project --python-preference only-managed python
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
# `auth status` names the store by these prefixes; each kind pins one store so
# a run that silently fell back to another store fails instead of passing.
# Values are (store prefix, expected file_backed).
KEY_STORE_PREFIXES = {
    "key-store-keyring": ("OS keyring item ", False),
    "key-store-file": ("key file ", True),
}


class ValidationError(ValueError):
    """Reported when command output cannot prove the expected live operation."""


def _validate_diagnostics(value):
    valid = (
        isinstance(value, list)
        and bool(value)
        and all(
            isinstance(item, dict) and item.get("status") in PASSING_STATUSES
            for item in value
        )
    )
    if not valid:
        raise ValidationError(
            "diagnostics must be a non-empty array without failing statuses"
        )


def _validate_catalog(value):
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


def _validate_list(value, provider):
    valid = isinstance(value, list) and all(
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
    if not valid:
        raise ValidationError(
            "inventory candidates must prove provider ownership and local state"
        )


def _validate_namespace_created(value, namespace):
    if not isinstance(value, dict) or value.get("status") != "created":
        raise ValidationError("namespace create status mismatch")
    if value.get("namespace") != namespace:
        raise ValidationError("namespace mismatch")


def _validate_doctor_counts(counts):
    if not isinstance(counts, dict) or any(
        type(counts.get(field)) is not int or counts[field] < 0
        for field in DOCTOR_COUNT_FIELDS
    ):
        raise ValidationError("doctor summary identity, report path or counts invalid")
    # Omitted pass details need positive, internally consistent check counts.
    if counts["total"] <= 0 or counts["total"] != sum(
        counts[status] for status in DOCTOR_STATUSES
    ):
        raise ValidationError("doctor summary must account for non-empty checks")


def _validate_doctor_results(results, counts):
    if not isinstance(results, list) or any(
        not isinstance(item, dict) or item.get("status") not in DOCTOR_DETAIL_STATUSES
        for item in results
    ):
        raise ValidationError("doctor summary results must contain only warn/fail/skip")
    if any(
        sum(item["status"] == status for item in results) != counts[status]
        for status in DOCTOR_DETAIL_STATUSES
    ):
        raise ValidationError("doctor summary result counts mismatch")


def _validate_doctor_summary(value, results, namespace, server):
    if (
        value.get("namespace") != namespace
        or value.get("server") != server
        or not isinstance(value.get("report_path"), str)
        or not value["report_path"].strip()
    ):
        raise ValidationError("doctor summary identity, report path or counts invalid")
    counts = value.get("counts")
    _validate_doctor_counts(counts)
    _validate_doctor_results(results, counts)
    expected_status = "fail" if counts["fail"] else "warn" if counts["warn"] else "pass"
    if value.get("status") != expected_status or counts["fail"]:
        raise ValidationError("doctor summary status mismatch or failing checks")


def _validate_doctor_report(value, namespace, server):
    if not isinstance(value, dict):
        raise ValidationError("doctor report must be an object")
    results = value.get("results")
    # Summary metadata must not bypass validation through the legacy report path.
    if any(field in value for field in DOCTOR_SUMMARY_FIELDS):
        _validate_doctor_summary(value, results, namespace, server)
        return
    valid = (
        isinstance(results, list)
        and bool(results)
        and all(
            isinstance(item, dict) and item.get("status") in PASSING_STATUSES
            for item in results
        )
    )
    if not valid:
        raise ValidationError(
            "doctor report must contain results without failing statuses"
        )


def _validate_server_status(value, provider, namespace, server):
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


def _validate_power(kind, value, provider, namespace, server):
    # Power commands and status share one row; a settled state, not an
    # in-between label such as "stopping", proves the operation finished.
    _validate_server_status(value, provider, namespace, server)
    expected = POWER_ON_LABELS if kind == "power-on" else POWER_OFF_LABELS
    if value["power"] not in expected:
        raise ValidationError(f"power {value['power']!r} is not settled {kind}")


def _validate_import_complete(value, provider, namespace, server):
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
        raise ValidationError(
            "import must report exactly one imported row for the server"
        )


def _validate_action_complete(action, value, provider, namespace, server):
    if (
        not isinstance(value, dict)
        or value.get("status") != "complete"
        or value.get("action") != action
    ):
        raise ValidationError(f"{action} completion mismatch")
    if value.get("namespace") != namespace:
        raise ValidationError("namespace mismatch")
    if value.get("server") != server:
        raise ValidationError("server mismatch")
    if action == "delete" and value.get("provider") != provider:
        raise ValidationError("provider mismatch")


def validate_output(kind, value, provider="", namespace="", server=""):
    """Validate one parsed command result against its semantic contract."""
    validators = {
        "diagnostics": lambda: _validate_diagnostics(value),
        "catalog": lambda: _validate_catalog(value),
        "list": lambda: _validate_list(value, provider),
        "namespace-created": lambda: _validate_namespace_created(value, namespace),
        "doctor-report": lambda: _validate_doctor_report(value, namespace, server),
        "server-status": lambda: _validate_server_status(
            value, provider, namespace, server
        ),
        "power-on": lambda: _validate_power(kind, value, provider, namespace, server),
        "power-off": lambda: _validate_power(kind, value, provider, namespace, server),
        "import-complete": lambda: _validate_import_complete(
            value, provider, namespace, server
        ),
        "bootstrap-complete": lambda: _validate_action_complete(
            "bootstrap", value, provider, namespace, server
        ),
        "delete-complete": lambda: _validate_action_complete(
            "delete", value, provider, namespace, server
        ),
    }
    validator = validators.get(kind)
    if validator is None:
        raise ValidationError(f"unknown output validator: {kind}")
    validator()


def validate_key_store(kind, value):
    """Prove `auth status` shows the run's key in its pinned store and every
    credential file encrypted. Kept apart from validate_output so the store
    contract stays small and testable on its own."""
    prefix, file_backed = KEY_STORE_PREFIXES[kind]
    if (
        not isinstance(value, dict)
        or value.get("key_present") is not True
        or value.get("error")
    ):
        raise ValidationError("credential key not readable")
    store = value.get("store")
    if (
        not isinstance(store, str)
        or not store.startswith(prefix)
        or value.get("file_backed") is not file_backed
    ):
        raise ValidationError("credential key is not in the expected store")
    encrypted = value.get("encrypted_files")
    if (
        not isinstance(encrypted, int)
        or encrypted < 1
        or value.get("plaintext_files") != 0
        or value.get("unreadable_files") != 0
    ):
        raise ValidationError("credential files are not all encrypted")


def validate_result(kind, value, provider="", namespace="", server=""):
    """Route a parsed command result to its validator; the single entry point
    for callers, so new contracts can live in their own functions."""
    if kind in KEY_STORE_PREFIXES:
        validate_key_store(kind, value)
        return
    validate_output(kind, value, provider, namespace, server)


def validate_file(kind, path, provider="", namespace="", server=""):
    """Load one complete JSON document before applying semantic validation."""
    try:
        with Path(path).open(encoding="utf-8") as stream:
            value = json.load(stream)
    except (OSError, json.JSONDecodeError) as error:
        raise ValidationError(f"invalid JSON output: {error}") from error
    validate_result(kind, value, provider, namespace, server)


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
    print(
        int((datetime.now(timezone.utc) - created).total_seconds() // SECONDS_PER_HOUR)
    )


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


def write_credentials(path, namespace, server, provider):
    """Write the CLI credential document with tokens taken from the environment.

    The file is created private, so no other user can read it even briefly.
    """
    payload = {
        "namespace": namespace,
        "server": server,
        "server_provider_token": os.environ[
            CREDENTIAL_TOKEN_ENV["server_provider_token"]
        ],
        "server_provider_name": provider,
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
    "write-credentials": (write_credentials, 4),
}


def run_helper(name, args):
    """Run one harness helper subcommand as the shell harness boundary."""
    helper, arity = HELPER_COMMANDS[name]
    if len(args) != arity:
        print(
            f"usage: dogfood_validate.py {name} takes {arity} argument(s)",
            file=sys.stderr,
        )
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
