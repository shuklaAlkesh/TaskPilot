"""Submit and track jobs from application code (the replacement for Celery's .delay / apply_async),
and manage recurring schedules (the replacement for celery beat).

    from taskpilot_worker.producer import TaskPilot

    tp = TaskPilot("https://taskpilot.example.com", api_key=os.environ["TASKPILOT_API_KEY"])
    job = tp.submit(
        "analytics.compute_usecase_metrics", {"usecase_id": "u1"}, idempotency_key="u1-day-1"
    )
    tp.wait(job["id"])

Synchronous on purpose: it is called from web request handlers. Uses the public API, so it needs
a tenant API key with jobs:write, never the worker token.
"""

import time
from collections.abc import Mapping
from datetime import datetime, timedelta, timezone
from typing import Any

import httpx

TERMINAL = frozenset({"succeeded", "failed", "cancelled"})


class TaskPilotError(Exception):
    """An API problem response (RFC 9457): status, code, and field errors."""

    def __init__(self, status: int, problem: Mapping[str, Any]) -> None:
        self.status = status
        self.code = str(problem.get("code", "unknown"))
        self.problem = dict(problem)
        super().__init__(
            f"{status} {self.code}: {problem.get('title', '')} {problem.get('errors', '')}".strip()
        )


class TaskPilot:
    """Thin client for the public /v1 API."""

    def __init__(
        self,
        base_url: str,
        api_key: str,
        timeout: float = 10.0,
        transport: httpx.BaseTransport | None = None,
    ) -> None:
        self._http = httpx.Client(
            base_url=base_url,
            headers={"Authorization": f"Bearer {api_key}"},
            timeout=timeout,
            transport=transport,
        )

    def __enter__(self) -> "TaskPilot":
        return self

    def __exit__(self, *_: object) -> None:
        self.close()

    def close(self) -> None:
        """Close the connection pool."""
        self._http.close()

    def _call(self, method: str, path: str, **kwargs: Any) -> dict[str, Any]:
        r = self._http.request(method, path, **kwargs)
        body: dict[str, Any] = r.json() if r.content else {}
        if r.is_error:
            raise TaskPilotError(r.status_code, body)
        return body

    def submit(
        self,
        task_type: str,
        payload: Mapping[str, Any],
        *,
        version: int = 1,
        queue: str | None = None,
        idempotency_key: str | None = None,
        delay_seconds: float | None = None,
        max_attempts: int | None = None,
        timeout_seconds: int | None = None,
    ) -> dict[str, Any]:
        """Queue a job and return it (status "queued"). Pass an idempotency_key so a retried
        submit returns the original job instead of creating a duplicate."""
        body: dict[str, Any] = {
            "task_type": task_type,
            "task_version": version,
            "payload": dict(payload),
        }
        if queue is not None:
            body["queue"] = queue
        if max_attempts is not None:
            body["max_attempts"] = max_attempts
        if timeout_seconds is not None:
            body["timeout_seconds"] = timeout_seconds
        if delay_seconds:
            run_at = datetime.now(timezone.utc) + timedelta(seconds=delay_seconds)
            body["run_at"] = run_at.isoformat()
        headers = {"Idempotency-Key": idempotency_key} if idempotency_key else {}
        return self._call("POST", "/v1/jobs", json=body, headers=headers)

    def get(self, job_id: str) -> dict[str, Any]:
        """The job, including its result once succeeded."""
        return self._call("GET", f"/v1/jobs/{job_id}")

    def cancel(self, job_id: str) -> dict[str, Any]:
        """Cancel a queued job, or ask the worker to stop a running one (best-effort)."""
        return self._call("POST", f"/v1/jobs/{job_id}/cancel")

    def put_schedule(
        self,
        name: str,
        task_type: str,
        payload: Mapping[str, Any],
        *,
        every_seconds: int | None = None,
        cron: str | None = None,
        tz: str = "UTC",
        version: int = 1,
        queue: str | None = None,
        enabled: bool = True,
        allow_overlap: bool = False,
        max_attempts: int | None = None,
        timeout_seconds: int | None = None,
    ) -> dict[str, Any]:
        """Create or replace the schedule `name` (the replacement for a Celery beat entry).

        Give exactly one of every_seconds or cron (5 fields, e.g. "0 18 * * *", evaluated in tz).
        A run is skipped while the previous job is still queued or running unless allow_overlap.
        Re-sending an unchanged schedule keeps its next run time, so call this on every deploy."""
        body: dict[str, Any] = {
            "task_type": task_type,
            "task_version": version,
            "payload": dict(payload),
            "timezone": tz,
            "enabled": enabled,
            "allow_overlap": allow_overlap,
        }
        optional = {
            "interval_seconds": every_seconds,
            "cron": cron,
            "queue": queue,
            "max_attempts": max_attempts,
            "timeout_seconds": timeout_seconds,
        }
        body |= {k: v for k, v in optional.items() if v is not None}
        return self._call("PUT", f"/v1/schedules/{name}", json=body)

    def list_schedules(self) -> list[dict[str, Any]]:
        """Every schedule of this tenant, ordered by name."""
        return list(self._call("GET", "/v1/schedules")["items"])

    def delete_schedule(self, name: str) -> None:
        """Remove a schedule. Jobs it already submitted are unaffected."""
        self._call("DELETE", f"/v1/schedules/{name}")

    def wait(self, job_id: str, timeout: float = 60.0, poll: float = 0.5) -> dict[str, Any]:
        """Poll until the job is succeeded, failed, or cancelled; TimeoutError after `timeout`
        seconds."""
        deadline = time.monotonic() + timeout
        while True:
            job = self.get(job_id)
            if job["status"] in TERMINAL:
                return job
            if time.monotonic() >= deadline:
                raise TimeoutError(f"job {job_id} still {job['status']} after {timeout}s")
            time.sleep(poll)
