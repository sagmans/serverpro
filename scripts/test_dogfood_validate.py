#!/usr/bin/env python3
"""Unit tests for live dogfood JSON output contracts."""

import copy
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
VALID_SUMMARY = {
    "namespace": NAMESPACE,
    "server": SERVER,
    "status": "pass",
    "counts": {"pass": 2, "warn": 0, "fail": 0, "skip": 0, "total": 2},
    "results": [],
    "report_path": "/tmp/doctor-report.json",
}
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

    def test_accepts_summary_with_omitted_pass_results(self):
        # Empty details prove success only when counts account for completed checks.
        for passing, statuses, expected in (
            (2, [], "pass"),
            (2, ["skip"], "pass"),
            (0, ["skip"], "pass"),
            (2, ["skip", "warn", "skip"], "warn"),
        ):
            with self.subTest(statuses=statuses):
                value = copy.deepcopy(VALID_SUMMARY)
                value["status"] = expected
                value["counts"]["pass"] = passing
                value["counts"]["total"] = passing
                value["results"] = [{"status": status} for status in statuses]
                for status in statuses:
                    value["counts"][status] += 1
                    value["counts"]["total"] += 1
                dogfood_validate.validate_output(
                    "doctor-report", value, PROVIDER, NAMESPACE, SERVER
                )

    def test_rejects_malformed_or_failing_summaries(self):
        invalid = []
        for field in VALID_SUMMARY:
            value = copy.deepcopy(VALID_SUMMARY)
            del value[field]
            invalid.append((f"missing {field}", value))
        for field, replacement in (
            ("namespace", "wrong"), ("server", "wrong"),
            ("status", "warn"), ("status", "fail"), ("status", "skip"),
            ("status", []), ("report_path", ""), ("report_path", "  "),
            ("report_path", None), ("counts", None), ("results", None),
        ):
            value = copy.deepcopy(VALID_SUMMARY)
            value[field] = replacement
            invalid.append((f"invalid {field}: {replacement!r}", value))
        for field in VALID_SUMMARY["counts"]:
            value = copy.deepcopy(VALID_SUMMARY)
            del value["counts"][field]
            invalid.append((f"missing count {field}", value))
            for count in (-1, True, "2", 2.0, None):
                value = copy.deepcopy(VALID_SUMMARY)
                value["counts"][field] = count
                invalid.append((f"invalid count {field}: {count!r}", value))
        for counts in (
            {"pass": 0, "warn": 0, "fail": 0, "skip": 0, "total": 0},
            {"pass": 2, "warn": 0, "fail": 0, "skip": 0, "total": 3},
            {"pass": 2, "warn": 1, "fail": 0, "skip": 0, "total": 3},
            {"pass": 2, "warn": 0, "fail": 0, "skip": 1, "total": 3},
            {"pass": 2, "warn": 0, "fail": 1, "skip": 0, "total": 3},
        ):
            value = copy.deepcopy(VALID_SUMMARY)
            value["counts"] = counts
            invalid.append((f"unproven counts {counts}", value))
        for result in ({"status": "pass"}, {"status": "warn"},
                       {"status": "skip"}, {"status": "fail"},
                       {"status": "unknown"}, {"status": []}, {}, None):
            value = copy.deepcopy(VALID_SUMMARY)
            value["results"] = [result]
            invalid.append((f"invalid result {result}", value))
        value = copy.deepcopy(VALID_SUMMARY)
        value["results"] = [{"status": "warn"}]
        value["counts"].update(warn=1, total=3)
        invalid.append(("hidden warning", value))
        # A coherent failing report still cannot prove a successful dogfood run.
        value = copy.deepcopy(VALID_SUMMARY)
        value.update(status="fail", results=[{"status": "fail"}])
        value["counts"].update(fail=1, total=3)
        invalid.append(("honest failure", value))
        value = copy.deepcopy(value)
        value["status"] = "pass"
        invalid.append(("hidden failure", value))
        for label, value in invalid:
            with self.subTest(label=label):
                with self.assertRaises(dogfood_validate.ValidationError):
                    dogfood_validate.validate_output(
                        "doctor-report", value, PROVIDER, NAMESPACE, SERVER
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
