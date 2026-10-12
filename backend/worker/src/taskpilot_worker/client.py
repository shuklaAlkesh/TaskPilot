"""HTTP client for the Go internal worker API (/internal/v1)."""

import asyncio
import logging
import time
from typing import Any
from uuid import UUID

import httpx

from taskpilot_worker.contract import Claim, Lease

logger = logging.getLogger(__name__)

REQUEST_TIMEOUT_SECONDS = 10.0
MAX_RETRY_DELAY_SECONDS = 5.0


class AttemptGoneError(Exception):
    """The server no longer accepts reports for this attempt (409 stale, or 404). Drop it."""


class WorkerClient:
    """Thin async client. Reports are retried on network errors and 5xx until a deadline."""

    def __init__(
        self, base_url: str, token: str, transport: httpx.AsyncBaseTransport | None = None
    ) -> None:
        self._http = httpx.AsyncClient(
            base_url=base_url,
            headers={"Authorization": f"Bearer {token}"},
            timeout=REQUEST_TIMEOUT_SECONDS,
            transport=transport,
        )

    async def aclose(self) -> None:
        """Close the connection pool."""
        await self._http.aclose()

    async def register(self, worker: dict[str, Any]) -> None:
        """Announce this worker and its task type definitions (POST /internal/v1/workers).

        Raises httpx.HTTPStatusError on rejection; the problem body says which definition and why.
        """
        r = await self._http.post("/internal/v1/workers", json=worker)
        r.raise_for_status()

    async def claim(
        self, worker_id: str, queues: list[str], supported: list[dict[str, Any]]
    ) -> Claim | None:
        """Lease the next job from one of `queues`, or return None when nothing is ready."""
        r = await self._http.post(
            "/internal/v1/claims",
            json={"worker_id": worker_id, "queues": queues, "supported": supported},
        )
        if r.status_code == httpx.codes.NO_CONTENT:
            return None
        r.raise_for_status()
        return Claim.model_validate_json(r.content)

    async def heartbeat(self, attempt_id: UUID) -> Lease:
        """Extend the lease. Raises AttemptGoneError if the attempt is no longer current."""
        r = await self._post(f"/internal/v1/attempts/{attempt_id}/heartbeat", {})
        return Lease.model_validate_json(r.content)

    async def report(
        self, attempt_id: UUID, kind: str, body: dict[str, Any], give_up_at: float
    ) -> None:
        """Send complete/fail/cancelled, retrying transient errors until give_up_at (monotonic).

        Repeating a report is safe: the server treats an identical repeat as a no-op.
        """
        delay = 0.5
        while True:
            try:
                await self._post(f"/internal/v1/attempts/{attempt_id}/{kind}", body)
                return
            except httpx.HTTPStatusError as e:
                if e.response.status_code < httpx.codes.INTERNAL_SERVER_ERROR:
                    raise
                err: Exception = e
            except httpx.TransportError as e:
                err = e
            if time.monotonic() + delay > give_up_at:
                raise err
            logger.warning("report failed, retrying", extra={"error": type(err).__name__})
            await asyncio.sleep(delay)
            delay = min(delay * 2, MAX_RETRY_DELAY_SECONDS)

    async def _post(self, path: str, body: dict[str, Any]) -> httpx.Response:
        r = await self._http.post(path, json=body)
        if r.status_code in (httpx.codes.NOT_FOUND, httpx.codes.CONFLICT):
            raise AttemptGoneError(r.json().get("code", str(r.status_code)))
        r.raise_for_status()
        return r
