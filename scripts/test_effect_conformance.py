"""Mapping admission controls; behavior evidence runs through existing Go tests."""

from __future__ import annotations

import copy
import unittest

import check_effect_conformance as mapping


class MappingTests(unittest.TestCase):
    def setUp(self) -> None:
        self.value = mapping.read_mapping(mapping.MAPPING)

    def reject(self, value: dict) -> None:
        with self.assertRaises(mapping.corpus.ImportError):
            mapping.validate_mapping(value)

    def test_current_selected_cases_have_real_evidence_targets(self) -> None:
        self.assertEqual(mapping.validate_mapping(self.value), [
            "TestExplicitTestProvidersUseTheHarnessAcrossTargets",
            "TestLifecycleConformanceAcrossGoAndEffect",
            "TestSchedulerTimerFailureIsPreservedAcrossTargets",
        ])

    def test_pin_and_duplicate_ids_fail(self) -> None:
        bad = copy.deepcopy(self.value)
        bad["sourceCommit"] = "0" * 40
        self.reject(bad)
        bad = copy.deepcopy(self.value)
        bad["cases"][1]["id"] = bad["cases"][0]["id"]
        self.reject(bad)

    def test_missing_changed_or_duplicate_upstream_case_fails(self) -> None:
        for field, value in (("file", "missing.test.ts"), ("file", "../outside"), ("line", 2734), ("label", "invented case")):
            with self.subTest(field=field, value=value):
                bad = copy.deepcopy(self.value)
                bad["cases"][0]["upstream"][field] = value
                self.reject(bad)
        bad = copy.deepcopy(self.value)
        bad["cases"][1]["upstream"] = bad["cases"][0]["upstream"]
        self.reject(bad)

    def test_invalid_native_evidence_targets_fail(self) -> None:
        for field, value in (("file", "internal/compiler/missing_test.go"), ("file", "../outside"), ("test", "TestImaginaryConformance"), ("targets", ["go"])):
            with self.subTest(field=field, value=value):
                bad = copy.deepcopy(self.value)
                bad["cases"][0]["evidence"][0][field] = value
                self.reject(bad)

    def test_reference_only_cases_cannot_be_called_covered_without_evidence(self) -> None:
        for status in ("covered", "difference", "passing"):
            with self.subTest(status=status):
                bad = copy.deepcopy(self.value)
                bad["cases"][4]["status"] = status
                self.reject(bad)
        for status in ("pending", "unsupported"):
            with self.subTest(status=status):
                bad = copy.deepcopy(self.value)
                bad["cases"][0]["status"] = status
                self.reject(bad)

    def test_behavior_limits_and_closed_schema_are_required(self) -> None:
        for field in ("behavior", "limits"):
            bad = copy.deepcopy(self.value)
            bad["cases"][0][field] = ""
            self.reject(bad)
        bad = copy.deepcopy(self.value)
        bad["cases"][0]["passedUpstreamSuite"] = True
        self.reject(bad)


if __name__ == "__main__":
    unittest.main()
