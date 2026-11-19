import json
import uuid
from datetime import datetime, timezone
from typing import Any

from taskpilot_worker import child
from taskpilot_worker.handlers.builtin import registry
from taskpilot_worker.registry import HandlerContext, Payload, TaskRegistry


def envelope(task_type: str = "system.echo", payload: dict[str, Any] | None = None) -> bytes:
    return json.dumps(
        {
            "schema_version": 1,
            "job_id": str(uuid.uuid4()),
            "attempt_id": str(uuid.uuid4()),
            "attempt_no": 1,
            "max_attempts": 3,
            "task_type": task_type,
            "task_version": 1,
            "timeout_seconds": 10,
            "payload": {"message": "hi"} if payload is None else payload,
            "created_at": datetime.now(timezone.utc).isoformat(),
        }
    ).encode()


def test_run_success() -> None:
    assert child.run(envelope(), registry) == {"ok": True, "result": {"message": "hi"}}


def test_run_unsupported_task_is_permanent() -> None:
    out = child.run(envelope("system.nope"), registry)
    assert out["error"]["code"] == "unsupported_task"
    assert out["error"]["retryable"] is False


def test_run_invalid_payload_is_permanent() -> None:
    out = child.run(envelope(payload={"message": ""}), registry)
    assert out["error"] == {
        "code": "invalid_payload",
        "message": "1 payload validation error(s)",
        "retryable": False,
    }


def test_run_malformed_envelope() -> None:
    assert child.run(b"not json", registry)["error"]["code"] == "invalid_envelope"


def test_run_unclassified_exception_is_retryable() -> None:
    boom_registry = TaskRegistry()

    class Empty(Payload):
        pass

    @boom_registry.task("system.echo")
    def boom(_p: Empty, _c: HandlerContext) -> dict[str, Any]:
        raise KeyError("secret payload value")

    out = child.run(envelope(payload={}), boom_registry)
    assert out["error"] == {
        "code": "handler_error",
        "message": "unhandled KeyError",
        "retryable": True,
    }
