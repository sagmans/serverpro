#!/usr/bin/env python3
"""Unit tests for live dogfood JSON output contracts."""

import json
import os
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(__file__))

import dogfood_validate

PROVIDER = "hetzner"
NAMESPACE = "spdogfood"
SERVER = "web"
VALID_CANDIDATE = {
    "provider": PROVIDER,
    "id": "123",
    "name": "spdogfood-web",
    "namespace": NAMESPACE,
    "server": SERVER,
    "labels_ok": True,
    "local_state": "missing",
}
VALID_OUTPUTS = {
    "diagnostics": [{"status": "pass"}, {"status": "warn"}],
    "catalog": [{"name": "fsn1"}],
    "list": [VALID_CANDIDATE],
    "namespace-created": {"status": "created", "namespace": NAMESPACE},
    "doctor-report": {"results": [{"status": "pass"}, {"status": "warn"}]},
    "server-status": {
        "namespace": NAMESPACE,
        "server": SERVER,
        "provider": PROVIDER,
        "power": "running",
    },
    "bootstrap-complete": {
        "status": "complete",
        "action": "bootstrap",
        "namespace": NAMESPACE,
        "server": SERVER,
    },
    "delete-complete": {
        "status": "complete",
        "action": "delete",
        "namespace": NAMESPACE,
        "server": SERVER,
        "provider": PROVIDER,
    },
}
CONTRACT_FIELD_MUTATIONS = {
    "namespace-created": {
        "status": "planned",
        "namespace": "wrong",
    },
    "server-status": {
        "namespace": "wrong",
        "server": "wrong",
        "provider": "wrong",
        "power": "",
    },
    "bootstrap-complete": {
        "status": "planned",
        "action": "planned",
        "namespace": "wrong",
        "server": "wrong",
    },
    "delete-complete": {
        "status": "planned",
        "action": "planned",
        "namespace": "wrong",
        "server": "wrong",
        "provider": "wrong",
    },
}
INVALID_OUTPUTS = {
    "diagnostics": [{"status": "fail"}],
    "catalog": [{"name": ""}],
    "list": [1],
    "namespace-created": {"status": "created", "namespace": "wrong"},
    "doctor-report": {"results": [{"status": "fail"}]},
    "server-status": {
        "namespace": NAMESPACE,
        "server": SERVER,
        "provider": "wrong",
        "power": "running",
    },
    "bootstrap-complete": {
        "status": "complete",
        "action": "planned",
        "namespace": NAMESPACE,
        "server": SERVER,
    },
    "delete-complete": {
        "status": "complete",
        "action": "delete",
        "namespace": NAMESPACE,
        "server": "wrong",
        "provider": PROVIDER,
    },
}


class ValidateOutputTests(unittest.TestCase):
    def test_accepts_every_output_contract(self):
        for kind, value in VALID_OUTPUTS.items():
            with self.subTest(kind=kind):
                dogfood_validate.validate_output(
                    kind, value, PROVIDER, NAMESPACE, SERVER
                )

    def test_rejects_every_invalid_output_contract(self):
        for kind, value in INVALID_OUTPUTS.items():
            with self.subTest(kind=kind):
                with self.assertRaises(dogfood_validate.ValidationError):
                    dogfood_validate.validate_output(
                        kind, value, PROVIDER, NAMESPACE, SERVER
                    )

    def test_accepts_empty_inventory_and_every_local_state(self):
        dogfood_validate.validate_output(
            "list", [], PROVIDER, NAMESPACE, SERVER
        )
        for local_state in ("missing", "partial", "present"):
            with self.subTest(local_state=local_state):
                candidate = dict(VALID_CANDIDATE, local_state=local_state)
                dogfood_validate.validate_output(
                    "list", [candidate], PROVIDER, NAMESPACE, SERVER
                )

    def test_rejects_incomplete_or_wrong_provider_inventory_candidates(self):
        invalid_candidates = []
        for field in (
            "provider",
            "id",
            "name",
            "namespace",
            "server",
            "labels_ok",
            "local_state",
        ):
            candidate = dict(VALID_CANDIDATE)
            candidate.pop(field)
            invalid_candidates.append((f"missing {field}", candidate))
        for field in ("id", "name", "namespace", "server"):
            for invalid_value in ("", "  ", 123):
                candidate = dict(VALID_CANDIDATE)
                candidate[field] = invalid_value
                invalid_candidates.append(
                    (f"invalid {field} {invalid_value!r}", candidate)
                )
        invalid_candidates.extend(
            (
                ("wrong provider", dict(VALID_CANDIDATE, provider="vultr")),
                ("invalid namespace", dict(VALID_CANDIDATE, namespace="../bad")),
                ("invalid server", dict(VALID_CANDIDATE, server="bad/path")),
                ("unmanaged", dict(VALID_CANDIDATE, labels_ok=False)),
                ("non-boolean ownership", dict(VALID_CANDIDATE, labels_ok=1)),
                ("invalid local state", dict(VALID_CANDIDATE, local_state="unknown")),
            )
        )

        for label, candidate in invalid_candidates:
            with self.subTest(label=label):
                with self.assertRaises(dogfood_validate.ValidationError):
                    dogfood_validate.validate_output(
                        "list", [candidate], PROVIDER, NAMESPACE, SERVER
                    )

    def test_inventory_ids_match_cli_grammar_boundaries(self):
        for field in ("namespace", "server"):
            for valid_value in ("a", "prod.api_1", "prod-api"):
                with self.subTest(field=field, valid=valid_value):
                    candidate = dict(VALID_CANDIDATE)
                    candidate[field] = valid_value
                    dogfood_validate.validate_output(
                        "list", [candidate], PROVIDER, NAMESPACE, SERVER
                    )
            for invalid_value in (
                ".prod",
                "prod.",
                "_prod",
                "prod_",
                "-prod",
                "prod-",
                "Prod",
                "bad/path",
                "bad\\path",
            ):
                with self.subTest(field=field, invalid=invalid_value):
                    candidate = dict(VALID_CANDIDATE)
                    candidate[field] = invalid_value
                    with self.assertRaises(dogfood_validate.ValidationError):
                        dogfood_validate.validate_output(
                            "list", [candidate], PROVIDER, NAMESPACE, SERVER
                        )

    def test_rejects_every_required_contract_field_mismatch(self):
        for kind, mutations in CONTRACT_FIELD_MUTATIONS.items():
            for field, invalid_value in mutations.items():
                with self.subTest(kind=kind, field=field):
                    value = dict(VALID_OUTPUTS[kind])
                    value[field] = invalid_value
                    with self.assertRaises(dogfood_validate.ValidationError):
                        dogfood_validate.validate_output(
                            kind, value, PROVIDER, NAMESPACE, SERVER
                        )

    def test_rejects_empty_collections_and_result_sets(self):
        for kind, value in (
            ("diagnostics", []),
            ("catalog", []),
            ("doctor-report", {"results": []}),
        ):
            with self.subTest(kind=kind):
                with self.assertRaises(dogfood_validate.ValidationError):
                    dogfood_validate.validate_output(
                        kind, value, PROVIDER, NAMESPACE, SERVER
                    )

    def test_rejects_unknown_validator(self):
        with self.assertRaisesRegex(
            dogfood_validate.ValidationError, "unknown output validator"
        ):
            dogfood_validate.validate_output(
                "unknown", {}, PROVIDER, NAMESPACE, SERVER
            )


class ValidateFileTests(unittest.TestCase):
    def test_loads_strict_json_before_validation(self):
        with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8") as output:
            json.dump(VALID_OUTPUTS["catalog"], output)
            output.flush()
            dogfood_validate.validate_file(
                "catalog", output.name, PROVIDER, NAMESPACE, SERVER
            )

    def test_rejects_malformed_json(self):
        with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8") as output:
            output.write("{\n")
            output.flush()
            with self.assertRaisesRegex(
                dogfood_validate.ValidationError, "invalid JSON output"
            ):
                dogfood_validate.validate_file(
                    "catalog", output.name, PROVIDER, NAMESPACE, SERVER
                )


class CommandTests(unittest.TestCase):
    def test_cli_reports_validation_failure(self):
        with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8") as output:
            json.dump(INVALID_OUTPUTS["server-status"], output)
            output.flush()
            result = subprocess.run(
                [
                    sys.executable,
                    dogfood_validate.__file__,
                    "server-status",
                    output.name,
                    PROVIDER,
                    NAMESPACE,
                    SERVER,
                ],
                check=False,
                capture_output=True,
                text=True,
            )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("provider mismatch", result.stderr)


if __name__ == "__main__":
    unittest.main()
