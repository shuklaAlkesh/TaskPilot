"""Supervisor tests: real handler subprocesses, fake Go API (httpx.MockTransport)."""

import asyncio
import json
import uuid
from datetime import UTC, datetime
from typing import Any

import httpx

from taskpilot_worker.client import WorkerClient
from taskpilot_worker.config import Config
from taskpilot_worker.contract import Claim
from taskpilot_worker.handlers.builtin import registry
from taskpilot_worker.supervisor import Supervisor, child_env, parse_child


class FakeAPI:
    """Records reports and answers heartbeats."""

    def __init__(self, cancel_requested: bool = False) -> None:
        self.cancel_requested = cancel_requested
        self.reports: list[tuple[str, dict[str, Any]]] = []

    def __call__(self, request: httpx.Request) -> httpx.Response:
        kind = request.url.path.rsplit("/", 1)[-1]
        if kind == "heartbeat":
            return httpx.Response(
                200, json={"lease_seconds": 30, "cancel_requested": self.cancel_requested}
            )
        self.reports.append((kind, json.loads(request.content)))
        return httpx.Response(204)


def make_claim(task_type: str, payload: dict[str, Any], timeout: int = 10) -> Claim:
    return Claim.model_validate(
        {
            "lease_seconds": 30,
            "heartbeat_interval_seconds": 1,
            "job": {
                "schema_version": 1,
                "job_id": str(uuid.uuid4()),
                "attempt_id": str(uuid.uuid4()),
                "attempt_no": 1,
                "max_attempts": 3,
                "task_type": task_type,
                "task_version": 1,
                "timeout_seconds": timeout,
                "payload": payload,
                "created_at": datetime.now(UTC).isoformat(),
            },
        }
    )


def run(api: FakeAPI, claim: Claim, stop: asyncio.Event | None = None) -> None:
    async def go() -> None:
        client = WorkerClient("http://api", "token", transport=httpx.MockTransport(api))
        sup = Supervisor(
            Config(api_url="http://api", worker_token="token", shutdown_grace=0),
            client,
            "w",
            registry,
        )
        try:
            await sup.run_attempt(claim, stop or asyncio.Event())
        finally:
            await client.aclose()

    asyncio.run(go())


def test_success_is_reported_as_complete() -> None:
    api = FakeAPI()
    run(api, make_claim("text.word_count", {"text": "a b a"}))
    assert api.reports == [
        (
            "complete",
            {
                "result": {
                    "words": 3,
                    "lines": 1,
                    "characters": 5,
                    "top_words": [{"word": "a", "count": 2}, {"word": "b", "count": 1}],
                }
            },
        )
    ]


def test_timeout_kills_child_and_reports_retryable() -> None:
    api = FakeAPI()
    run(api, make_claim("system.sleep", {"seconds": 30}, timeout=1))
    [(kind, body)] = api.reports
    assert kind == "fail"
    assert body["error"]["code"] == "timeout"
    assert body["error"]["retryable"] is True


def test_cancel_request_stops_child() -> None:
    api = FakeAPI(cancel_requested=True)
    run(api, make_claim("system.sleep", {"seconds": 30}))
    assert api.reports == [("cancelled", {})]


def test_crash_is_reported_as_handler_crashed() -> None:
    api = FakeAPI()
    run(api, make_claim("system.sleep", {"seconds": 0, "outcome": "crash"}))
    assert api.reports[0][1]["error"]["code"] == "handler_crashed"


def test_shutdown_reports_worker_shutdown() -> None:
    api = FakeAPI()
    stop = asyncio.Event()
    stop.set()
    run(api, make_claim("system.sleep", {"seconds": 30}), stop)
    assert api.reports[0][1]["error"]["code"] == "worker_shutdown"


def test_parse_child_rejects_contract_violations() -> None:
    assert parse_child(b"garbage\n", 0)[1]["error"]["code"] == "invalid_result"
    assert parse_child(b"", 0)[1]["error"]["code"] == "handler_crashed"
    big = json.dumps({"ok": True, "result": {"x": "y" * 70_000}}).encode()
    assert parse_child(big, 0)[1]["error"]["code"] == "result_too_large"


def test_register_announces_every_task_type_and_queue() -> None:
    api = FakeAPI()
    sent: list[dict[str, Any]] = []

    def capture(request: httpx.Request) -> httpx.Response:
        sent.append(json.loads(request.content))
        return httpx.Response(200, json={"registered": 4})

    async def go() -> None:
        client = WorkerClient("http://api", "token", transport=httpx.MockTransport(capture))
        cfg = Config(api_url="http://api", worker_token="token", concurrency=2)
        await Supervisor(cfg, client, "w-1", registry).register("host-a", "0.1.0")
        await client.aclose()

    asyncio.run(go())
    assert api.reports == []
    [body] = sent
    assert body["queues"] == ["default"]
    assert body["concurrency"] == 2
    assert [t["task_type"] for t in body["task_types"]] == [
        "data.csv_summary",
        "system.echo",
        "system.sleep",
        "text.word_count",
    ]


def test_child_env_passes_only_allow_listed_variables(monkeypatch: Any) -> None:
    monkeypatch.setenv("DJANGO_SETTINGS_MODULE", "livis.settings")
    monkeypatch.setenv("UNRELATED_SECRET", "nope")
    monkeypatch.setenv("TASKPILOT_WORKER_TOKEN", "never-in-child")
    cfg = Config(api_url="x", worker_token="t", child_env_allow=("DJANGO_SETTINGS_MODULE",))
    env = child_env(cfg)
    assert env["DJANGO_SETTINGS_MODULE"] == "livis.settings"
    assert env["TASKPILOT_WORKER_APP"] == cfg.app
    assert "UNRELATED_SECRET" not in env
    assert "never-in-child" not in env.values()


def test_bad_claim_response_does_not_kill_the_worker() -> None:
    calls = 0

    def handler(request: httpx.Request) -> httpx.Response:
        nonlocal calls
        calls += 1
        if calls == 1:
            return httpx.Response(200, json={"lease_seconds": 30, "job": {"unexpected": True}})
        stop.set()
        return httpx.Response(204)

    stop = asyncio.Event()

    async def go() -> None:
        client = WorkerClient("http://api", "token", transport=httpx.MockTransport(handler))
        cfg = Config(api_url="http://api", worker_token="token", concurrency=1, poll_interval=0.01)
        await Supervisor(cfg, client, "w", registry).run(stop)
        await client.aclose()

    asyncio.run(go())
    assert calls >= 2, "the slot kept claiming after the malformed response"


def test_long_timeouts_parse() -> None:
    claim = make_claim("system.echo", {"message": "x"}, timeout=86_400)
    assert claim.job.timeout_seconds == 86_400
