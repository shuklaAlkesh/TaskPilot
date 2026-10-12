"""Entry point: python -m taskpilot_worker."""

import asyncio
import json
import logging
import os
import secrets
import signal
import socket
import sys
from importlib.metadata import version
from typing import Any, ClassVar

import httpx

from taskpilot_worker.client import WorkerClient
from taskpilot_worker.config import Config
from taskpilot_worker.registry import TaskRegistry, load_registry
from taskpilot_worker.supervisor import Supervisor

logger = logging.getLogger("taskpilot_worker")


class JSONFormatter(logging.Formatter):
    """One JSON object per line with the allow-listed extra fields (docs/11 §2)."""

    FIELDS: ClassVar[tuple[str, ...]] = (
        "job_id",
        "attempt_id",
        "task_type",
        "attempt_no",
        "outcome",
        "error_code",
        "error",
        "stderr_bytes",
        "worker_id",
        "app",
        "queues",
        "task_types",
    )

    def format(self, record: logging.LogRecord) -> str:
        """Render the record as JSON."""
        out: dict[str, Any] = {
            "time": self.formatTime(record),
            "level": record.levelname,
            "logger": record.name,
            "msg": record.getMessage(),
        }
        out.update(
            {f: getattr(record, f) for f in self.FIELDS if getattr(record, f, None) is not None}
        )
        return json.dumps(out)


async def _register(supervisor: Supervisor, hostname: str, stop: asyncio.Event) -> bool:
    """Register, retrying with backoff while the API is unreachable (e.g. both just started).
    Returns False if asked to stop first. Exits if the API rejects the definitions."""
    delay = 1.0
    while True:
        try:
            await supervisor.register(hostname, version("taskpilot-worker"))
            return True
        except httpx.HTTPStatusError as e:
            if e.response.status_code < httpx.codes.INTERNAL_SERVER_ERROR:
                # A rejected definition is a deploy mistake (bad or changed schema): stop loudly.
                logger.error("registration rejected: %s", e.response.text)
                sys.exit(3)
            err: Exception = e
        except httpx.TransportError as e:
            err = e
        logger.warning(
            "API unreachable, retrying registration", extra={"error": type(err).__name__}
        )
        try:
            await asyncio.wait_for(stop.wait(), delay)
            return False
        except TimeoutError:
            delay = min(delay * 2, 30.0)


async def _serve(cfg: Config, registry: TaskRegistry) -> None:
    hostname = socket.gethostname()
    worker_id = f"{hostname}/{os.getpid()}/{secrets.token_hex(4)}"[:128]
    stop = asyncio.Event()
    loop = asyncio.get_running_loop()
    for sig in (signal.SIGTERM, signal.SIGINT):
        try:
            loop.add_signal_handler(sig, stop.set)
        except NotImplementedError:  # Windows: only SIGINT (Ctrl+C) can be caught
            signal.signal(sig, lambda *_: loop.call_soon_threadsafe(stop.set))
    client = WorkerClient(cfg.api_url, cfg.worker_token)
    supervisor = Supervisor(cfg, client, worker_id, registry)
    try:
        if not await _register(supervisor, hostname, stop):
            return
        logger.info(
            "worker started",
            extra={
                "worker_id": worker_id,
                "app": cfg.app,
                "queues": supervisor.queues,
                "task_types": [f"{s.task_type}@{s.version}" for s in registry.specs()],
            },
        )
        await supervisor.run(stop)
    finally:
        await client.aclose()


def main() -> None:
    """Configure logging, read config, and run until SIGTERM/SIGINT."""
    handler = logging.StreamHandler(sys.stdout)
    handler.setFormatter(JSONFormatter())
    logging.basicConfig(level=logging.INFO, handlers=[handler])
    # httpx request lines carry no allow-listed fields.
    logging.getLogger("httpx").setLevel(logging.WARNING)
    try:
        cfg = Config.from_env()
        registry = load_registry(cfg.app)
    except (ValueError, TypeError, ImportError) as e:
        logger.error("invalid configuration: %s", e)
        sys.exit(2)
    if not registry.specs():
        logger.error("%s registers no task types", cfg.app)
        sys.exit(2)
    asyncio.run(_serve(cfg, registry))


if __name__ == "__main__":
    main()
