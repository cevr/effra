"""Shared producer-aware parity assertions for process-spawned smoke tests."""
import copy
import re


_ARTIFACT_DIGEST = re.compile(r"^sha256:[0-9a-f]{64}$")


def adapter_semantic(value):
    """Remove only adapter envelope and byte-charge fields from a report."""
    result = {key: item for key, item in value.items() if key not in ("file", "timings")}
    if "typeProjectionUsage" in result:
        result["typeProjectionUsage"] = {
            key: item for key, item in result["typeProjectionUsage"].items()
            if key not in ("compatibilityBytes", "nameBytes", "responseBytes")}
    return result


def producer_snapshot(value, target=None, snapshot_schema=8):
    """Validate one response envelope and its independent snapshot epoch."""
    assert isinstance(value, dict)
    for key in ("schemaVersion", "revision", "target", "producer", "snapshot"):
        assert key in value, key
    expected_target = value["target"] if target is None else target
    assert value["target"] == expected_target, (value["target"], expected_target)
    producer = value["producer"]
    snapshot = value["snapshot"]
    assert isinstance(producer, dict) and isinstance(snapshot, dict)
    observed_report_schema = value["schemaVersion"]
    assert type(observed_report_schema) is int and observed_report_schema > 0, observed_report_schema
    observed_snapshot_schema = snapshot.get("schemaVersion")
    assert type(snapshot_schema) is int and snapshot_schema > 0, snapshot_schema
    for key in ("strength", "qualifier", "reuseScope"):
        assert key in producer, key
    scope = producer["reuseScope"]
    assert scope in ("artifact", "process", "none"), scope
    if scope == "artifact":
        assert producer["strength"] == "executing-artifact"
        assert _ARTIFACT_DIGEST.fullmatch(producer["digest"])
        assert producer["qualifier"] == producer["digest"]
        assert "reason" not in producer or producer["reason"] == ""
    else:
        assert producer["strength"] == "unavailable"
        assert "digest" not in producer or producer["digest"] == ""
        assert producer.get("reason"), producer
        if scope == "process":
            assert producer["qualifier"].startswith("process:")
            assert len(producer["qualifier"]) > len("process:")
        else:
            assert producer["qualifier"] == ""
    assert type(observed_snapshot_schema) is int and observed_snapshot_schema == snapshot_schema, (observed_snapshot_schema, snapshot_schema)
    assert snapshot == {
        "schemaVersion": snapshot_schema,
        "revision": value["revision"],
        "target": expected_target,
        "producer": producer["qualifier"],
        "reuseScope": scope,
    }, snapshot
    return copy.deepcopy(producer)


def _project(value, ignored, project):
    result = copy.deepcopy(value)
    for key in ignored:
        result.pop(key, None)
    return project(result) if project is not None else result


def assert_report_parity(actual, expected, target=None, ignored=(), project=None,
                         report_schema=8, snapshot_schema=8):
    """Compare decorated reports without erasing their producer contract.

    The outer report and embedded semantic snapshot have separate epochs.
    Artifact-scoped reports are equal after only explicitly named adapter
    envelope projections. Process-scoped reports may differ only in the
    process qualifier; ``none`` reports retain exact metadata equality.
    """
    assert type(report_schema) is int and report_schema > 0, report_schema
    assert type(actual.get("schemaVersion")) is int and actual["schemaVersion"] == report_schema, actual
    assert type(expected.get("schemaVersion")) is int and expected["schemaVersion"] == report_schema, expected
    actual_producer = producer_snapshot(actual, target, snapshot_schema)
    expected_producer = producer_snapshot(expected, target, snapshot_schema)
    for key in ("schemaVersion", "revision", "target"):
        assert actual[key] == expected[key], (key, actual[key], expected[key])
    for key in ("source", "sources"):
        if key in actual or key in expected:
            assert key in actual and key in expected
            assert actual[key] == expected[key], (key, actual[key], expected[key])
    assert actual_producer["reuseScope"] == expected_producer["reuseScope"], (
        actual_producer, expected_producer)

    left = _project(actual, ignored, project)
    right = _project(expected, ignored, project)
    scope = actual_producer["reuseScope"]
    if scope == "process":
        for report in (left, right):
            report["producer"] = dict(report["producer"], qualifier="<process>")
            report["snapshot"] = dict(report["snapshot"], producer="<process>")
    assert left == right, (left, right)
