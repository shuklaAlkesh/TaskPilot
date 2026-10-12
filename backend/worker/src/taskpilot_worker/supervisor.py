"""Claim loop, heartbeats, timeouts, cancellation, and shutdown (docs/03 §4.2, docs/05 §6)."""

import asyncio
import contextlib
import enum
import json
import logging
import os
import sys
import time
from dataclasses import dataclass
from typing import Any

import httpx
from pydantic import ValidationError

from taskpilot_worker.client import AttemptGoneError, WorkerClient
from taskpilot_worker.config import Config
from taskpilot_worker.contract import MAX_RESULT_BYTES, ChildOutput, Claim
from taskpilot_worker.registry import TaskRegistry

logger = logging.getLogger(__name__)

# Fixed argv, never a shell. -I isolates the child from user site-packages and the environment.
CHILD_ARGV = (sys.executable, "-I", "-m", "taskpilot_worker.child")
KILL_GRACE_SECONDS = 5.0
SELF_FENCE_MARGIN_SECONDS = 5.0
WATCH_TICK_SECONDS = 0.5

# A report to send: (kind, body), where kind is "complete", "fail", or "cancelled".
Report = tuple[str, dict[str, Any]]


class Stop(enum.Enum):
    """Why the watcher wants the handler stopped."""

    CANCEL = "cancel"
    LOST = "lost"  # lease lost or attempt no longer current: drop silently
    SHUTDOWN = "shutdown"


@dataclass(slots=True)
class _Lease:
    last_renewed: float  # time.monotonic() of the last successful claim or heartbeat


def child_env(cfg: Config) -> dict[str, str]:
    """Minimal child environment plus the variables the operator allow-listed for handler code
    (TASKPILOT_WORKER_CHILD_ENV, e.g. DJANGO_SETTINGS_MODULE). The worker token is never passed."""
    env = {
        "PATH": os.environ.get("PATH", ""),
        "LANG": "C.UTF-8",
        "TASKPILOT_WORKER_APP": cfg.app,
        # "" means the current directory; make it absolute so the child (run with -I) keeps it.
        "TASKPILOT_CHILD_SYS_PATH": os.pathsep.join(os.path.abspath(p or ".") for p in sys.path),
    }
    if sys.platform == "win32":
        env["SYSTEMROOT"] = os.environ.get("SYSTEMROOT", r"C:\Windows")  # Python needs it to start
    env |= {name: os.environ[name] for name in cfg.child_env_allow if name in os.environ}
    return env


def _fail(code: str, message: str, retryable: bool) -> Report:
    return "fail", {"error": {"code": code, "message": message, "retryable": retryable}}


def parse_child(stdout: bytes, returncode: int | None) -> Report:
    """Turn the child's exit status and stdout line into a report."""
    lines = stdout.strip().splitlines()
    if returncode != 0 or not lines:
        return _fail("handler_crashed", f"handler process exited with code {returncode}", True)
    try:
        out = ChildOutput.model_validate_json(lines[-1])
    except ValidationError:
        return _fail("invalid_result", "handler output did not match the contract", False)
    if out.ok and out.result is not None:
        if len(json.dumps(out.result, separators=(",", ":"))) > MAX_RESULT_BYTES:
            return _fail("result_too_large", "result exceeds 64 KiB", False)
        return "complete", {"result": out.result}
    if not out.ok and out.error is not None:
        return "fail", {"error": out.error.model_dump()}
    return _fail("invalid_result", "handler output did not match the contract", False)


async def _kill(proc: asyncio.subprocess.Process) -> None:
    """SIGTERM, then SIGKILL after a grace period."""
    if proc.returncode is not None:
        return
    proc.terminate()
    try:
        await asyncio.wait_for(proc.wait(), KILL_GRACE_SECONDS)
    except TimeoutError:
        proc.kill()
        await proc.wait()


class Supervisor:
    """Runs `concurrency` slots; each slot claims and runs one attempt at a time."""

    def __init__(
        self, cfg: Config, client: WorkerClient, worker_id: str, registry: TaskRegistry
    ) -> None:
        self._cfg = cfg
        self._client = client
        self._worker_id = worker_id
        self._queues = list(cfg.queues) or registry.queues()
        self._supported = [
            {"task_type": s.task_type, "task_version": s.version} for s in registry.specs()
        ]
        self._registry = registry

    @property
    def queues(self) -> list[str]:
        """The queues this worker claims from."""
        return self._queues

    async def register(self, hostname: str, version: str) -> None:
        """Announce this worker and every task type it can run; the API makes them submittable."""
        await self._client.register(
            {
                "worker_id": self._worker_id,
                "hostname": hostname,
                "version": version,
                "concurrency": self._cfg.concurrency,
                "queues": self._queues,
                "task_types": [s.definition() for s in self._registry.specs()],
            }
        )

    async def run(self, stop: asyncio.Event) -> None:
        """Claim and run jobs until stop is set and in-flight attempts are finished or reported."""
        async with asyncio.TaskGroup() as tg:
            for _ in range(self._cfg.concurrency):
                tg.create_task(self._slot(stop))

    async def _slot(self, stop: asyncio.Event) -> None:
        while not stop.is_set():
            claim: Claim | None = None
            try:
                claim = await self._client.claim(self._worker_id, self._queues, self._supported)
            except httpx.HTTPError as e:
                logger.warning("claim failed", extra={"error": type(e).__name__})
            except ValidationError:
                # Contract mismatch with the API (e.g. a newer server). Keep serving; the leased
                # job is re-queued by the reaper when its lease expires.
                logger.error(
                    "claim response did not match the contract", extra={"error": "ValidationError"}
                )
            if claim is None:
                with contextlib.suppress(TimeoutError):
                    await asyncio.wait_for(stop.wait(), self._cfg.poll_interval)
                continue
            await self.run_attempt(claim, stop)

    async def run_attempt(self, claim: Claim, stop: asyncio.Event) -> None:
        """Run one attempt in a subprocess and report its outcome."""
        job = claim.job
        log = {
            "job_id": str(job.job_id),
            "attempt_id": str(job.attempt_id),
            "task_type": job.task_type,
            "attempt_no": job.attempt_no,
        }
        logger.info("attempt started", extra=log)
        lease = _Lease(time.monotonic())
        proc = await asyncio.create_subprocess_exec(
            *CHILD_ARGV,
            stdin=asyncio.subprocess.PIPE,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
            env=child_env(self._cfg),
        )
        comm = asyncio.create_task(proc.communicate(job.model_dump_json().encode()))
        watch = asyncio.create_task(self._watch(claim, lease, stop))
        done, _ = await asyncio.wait(
            {comm, watch}, timeout=job.timeout_seconds, return_when=asyncio.FIRST_COMPLETED
        )
        report: Report | None
        if comm in done:
            stdout, stderr = comm.result()
            report = parse_child(stdout, proc.returncode)
            if report[0] == "fail" and stderr:
                log["stderr_bytes"] = len(stderr)  # never the content: it may echo payload data
        else:
            reason = watch.result() if watch in done else None
            await _kill(proc)
            await comm
            report = {
                None: _fail("timeout", f"handler exceeded {job.timeout_seconds}s", True),
                Stop.CANCEL: ("cancelled", {}),
                Stop.SHUTDOWN: _fail("worker_shutdown", "worker stopped before the handler", True),
                Stop.LOST: None,
            }[reason]
        watch.cancel()
        with contextlib.suppress(asyncio.CancelledError):
            await watch
        await self._send(claim, report, lease, log)

    async def _watch(self, claim: Claim, lease: _Lease, stop: asyncio.Event) -> Stop:
        """Heartbeat, watch for cancel and shutdown, and self-fence when the lease is at risk."""
        interval = claim.heartbeat_interval_seconds
        next_beat = time.monotonic() + interval
        shutdown_at: float | None = None
        while True:
            await asyncio.sleep(WATCH_TICK_SECONDS)
            now = time.monotonic()
            if stop.is_set():
                shutdown_at = shutdown_at or now + self._cfg.shutdown_grace
                if now >= shutdown_at:
                    return Stop.SHUTDOWN
            if now >= next_beat:
                next_beat = now + interval
                try:
                    beat = await self._client.heartbeat(claim.job.attempt_id)
                except AttemptGoneError:
                    return Stop.LOST
                except httpx.HTTPError as e:
                    logger.warning("heartbeat failed", extra={"error": type(e).__name__})
                else:
                    lease.last_renewed = time.monotonic()
                    if beat.cancel_requested:
                        return Stop.CANCEL
            if now - lease.last_renewed > claim.lease_seconds - SELF_FENCE_MARGIN_SECONDS:
                logger.warning(
                    "lease at risk, self-fencing", extra={"attempt_id": str(claim.job.attempt_id)}
                )
                return Stop.LOST

    async def _send(
        self, claim: Claim, report: Report | None, lease: _Lease, log: dict[str, Any]
    ) -> None:
        if report is None:
            logger.warning("attempt dropped without a report", extra=log)
            return
        kind, body = report
        log = {**log, "outcome": kind, "error_code": body.get("error", {}).get("code")}
        try:
            await self._client.report(
                claim.job.attempt_id, kind, body, lease.last_renewed + claim.lease_seconds
            )
        except AttemptGoneError:
            logger.info("attempt no longer current, result discarded", extra=log)
        except httpx.HTTPError as e:
            logger.error("report failed", extra={**log, "error": type(e).__name__})
        else:
            logger.info("attempt finished", extra=log)
