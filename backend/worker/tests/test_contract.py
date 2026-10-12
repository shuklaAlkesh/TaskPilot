"""What the built-in registry tells the API at registration (ADR-0009)."""

from taskpilot_worker.handlers.builtin import registry
from taskpilot_worker.registry import NAME_PATTERN, QUEUE_PATTERN


def test_builtin_task_types() -> None:
    assert [s.key for s in registry.specs()] == [
        ("data.csv_summary", 1),
        ("system.echo", 1),
        ("system.sleep", 1),
        ("text.word_count", 1),
    ]


def test_definitions_are_complete_and_strict() -> None:
    """Every definition explains itself, ships a valid example, and rejects unknown fields."""
    for spec in registry.specs():
        d = spec.definition()
        assert NAME_PATTERN.match(d["task_type"])
        assert QUEUE_PATTERN.match(d["queue"])
        assert d["description"], spec.task_type
        assert d["example"] is not None, spec.task_type
        spec.payload_model.model_validate(d["example"])
        schema = d["payload_schema"]
        assert schema["type"] == "object"
        assert schema["additionalProperties"] is False
        assert all(p.get("description") for p in schema["properties"].values()), spec.task_type


def test_handlers_declare_safe_side_effects() -> None:
    """docs/05 §7: the built-in handlers are pure, so running them twice is harmless."""
    assert all(s.idempotent and not s.side_effects for s in registry.specs())
