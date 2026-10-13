import pytest

from perch_pulse_dbx.identifiers import (
    UnsafeIdentifierError,
    qualify_table,
    require_identifier,
    schema_for_domain,
    table_for_dataset,
)


def test_safe_identifiers():
    assert require_identifier("perch_pulse") == "perch_pulse"
    assert qualify_table("perch_pulse", "analytics_observed", "pulse_incidents").endswith(
        ".pulse_incidents"
    )


def test_rejects_unsafe():
    with pytest.raises(UnsafeIdentifierError):
        require_identifier("drop table;--")
    with pytest.raises(UnsafeIdentifierError):
        require_identifier("analytics-observed")
    with pytest.raises(UnsafeIdentifierError):
        table_for_dataset("not_a_dataset")


def test_domain_schema():
    assert schema_for_domain("analytics", "synthetic") == "analytics_synthetic"
    assert schema_for_domain("analytics", "observed") == "analytics_observed"
