import json
from typing import Any

import httpx
import pytest

from taskpilot_worker.producer import TaskPilot, TaskPilotError


def client(handler: Any) -> TaskPilot:
    return TaskPilot("http://api", "tp_key", transport=httpx.MockTransport(handler))


def test_submit_sends_job_options_and_idempotency_key() -> None:
    seen: list[httpx.Request] = []

    def handler(request: httpx.Request) -> httpx.Response:
        seen.append(request)
        return httpx.Response(202, json={"id": "j1", "status": "queued"})

    with client(handler) as tp:
        job = tp.submit(
            "analytics.compute_usecase_metrics",
            {"usecase_id": "u1"},
            queue="analytics",
            idempotency_key="u1-day-1",
            delay_seconds=30,
            max_attempts=5,
        )
    assert job["id"] == "j1"
    [req] = seen
    body = json.loads(req.content)
    assert req.headers["Authorization"] == "Bearer tp_key"
    assert req.headers["Idempotency-Key"] == "u1-day-1"
    assert body["queue"] == "analytics"
    assert body["max_attempts"] == 5
    assert "run_at" in body


def test_problem_responses_raise_with_code() -> None:
    def handler(_r: httpx.Request) -> httpx.Response:
        return httpx.Response(
            422, json={"code": "unknown_task_type", "title": "Task type is not registered"}
        )

    with client(handler) as tp, pytest.raises(TaskPilotError) as exc:
        tp.submit("nope.task", {})
    assert exc.value.status == 422
    assert exc.value.code == "unknown_task_type"


def test_wait_polls_until_terminal() -> None:
    states = iter(["queued", "running", "succeeded"])

    def handler(_r: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200, json={"id": "j1", "status": next(states), "result": {"ok": True}}
        )

    with client(handler) as tp:
        assert tp.wait("j1", poll=0)["status"] == "succeeded"
