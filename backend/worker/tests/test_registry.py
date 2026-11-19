import uuid
from typing import Any

import pytest

from taskpilot_worker.registry import HandlerContext, Payload, TaskRegistry, load_registry


class ReportV1(Payload):
    to: str


def test_decorator_registers_with_sensible_defaults() -> None:
    reg = TaskRegistry()

    @reg.task("notifications.tasks.send_report_digest", queue="notifications", timeout=300)
    def send(p: ReportV1, _ctx: HandlerContext) -> dict[str, Any]:
        """Emails the weekly report digest.

        Longer notes that are not part of the summary.
        """
        return {"sent_to": p.to}

    spec = reg.get("notifications.tasks.send_report_digest", 1)
    assert spec is not None
    assert spec.title == "Send report digest"
    assert spec.description == "Emails the weekly report digest."
    assert spec.max_timeout == 300
    assert reg.queues() == ["notifications"]
    d = spec.definition()
    assert d["payload_schema"]["required"] == ["to"]
    assert d["example"] is None
    assert send(ReportV1(to="ops"), HandlerContext(uuid.uuid4(), 1, 0.0)) == {"sent_to": "ops"}


@pytest.mark.parametrize(
    ("kwargs", "message"),
    [
        ({"queue": "Bad Queue"}, "queue"),
        ({"timeout": 90_000}, "timeout"),
        ({"max_attempts": 0}, "max_attempts"),
        ({"side_effects": True, "idempotent": False}, "side effects"),
        ({"example": {"to": 5}}, "validation error"),
    ],
)
def test_decorator_rejects_unsafe_or_invalid_definitions(
    kwargs: dict[str, Any], message: str
) -> None:
    reg = TaskRegistry()
    with pytest.raises((ValueError, TypeError), match=message):

        @reg.task("training.tasks.run_training_job", **kwargs)
        def run(p: ReportV1, _ctx: HandlerContext) -> dict[str, Any]:
            return {}


def test_decorator_rejects_bad_names_duplicates_and_untyped_payloads() -> None:
    reg = TaskRegistry()
    with pytest.raises(ValueError, match="name"):
        reg.task("run_training_job")(lambda p, c: {})

    @reg.task("training.run")
    def first(p: ReportV1, _ctx: HandlerContext) -> dict[str, Any]:
        return {}

    with pytest.raises(ValueError, match="already registered"):
        reg.task("training.run")(first)
    with pytest.raises(TypeError, match="pydantic model"):

        @reg.task("training.untyped")
        def untyped(p: dict[str, Any], _ctx: HandlerContext) -> dict[str, Any]:
            return {}


def test_non_idempotent_side_effect_allowed_with_one_attempt() -> None:
    reg = TaskRegistry()

    @reg.task("billing.charge_card", side_effects=True, idempotent=False, max_attempts=1)
    def charge(p: ReportV1, _ctx: HandlerContext) -> dict[str, Any]:
        return {}

    assert reg.get("billing.charge_card", 1) is not None


def test_load_registry() -> None:
    assert load_registry("taskpilot_worker.handlers.builtin:registry").get("system.echo", 1)
    with pytest.raises(TypeError, match="not a TaskRegistry"):
        load_registry("taskpilot_worker.config:BUILTIN_APP")
    with pytest.raises(ImportError):
        load_registry("no_such_module:registry")
