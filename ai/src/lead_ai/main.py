"""Service entry point: config, logging, gRPC server, graceful shutdown."""

from __future__ import annotations

import asyncio
import json
import logging
import signal
import sys
from typing import Any

import grpc
from grpc_health.v1 import health, health_pb2, health_pb2_grpc
from pydantic import ValidationError

from lead_ai import llm
from lead_ai.config import Settings, get_settings
from lead_ai.gen.leadintel.analysis.v1 import analysis_pb2_grpc as pb_grpc
from lead_ai.server import SERVICE_NAME, AnalysisServicer

_STD_ATTRS = set(logging.makeLogRecord({}).__dict__) | {"message", "asctime"}


class JsonFormatter(logging.Formatter):
    """One JSON object per line; ``extra=`` fields become top-level keys."""

    def format(self, record: logging.LogRecord) -> str:
        out: dict[str, Any] = {
            "time": self.formatTime(record, "%Y-%m-%dT%H:%M:%S%z"),
            "level": record.levelname,
            "logger": record.name,
            "msg": record.getMessage(),
        }
        out.update({k: v for k, v in record.__dict__.items() if k not in _STD_ATTRS})
        if record.exc_info:
            out["exc"] = self.formatException(record.exc_info)
        return json.dumps(out, default=str)


def setup_logging(level: str) -> None:
    handler = logging.StreamHandler(sys.stdout)
    handler.setFormatter(JsonFormatter())
    logging.basicConfig(level=level.upper(), handlers=[handler], force=True)
    # Request logs from the HTTP clients would include URLs; keep them quiet.
    for noisy in ("httpx", "httpcore", "openai"):
        logging.getLogger(noisy).setLevel(logging.WARNING)


async def check_llm(settings: Settings) -> None:
    """Startup reachability check: warn, never exit (the LLM server may start later)."""
    try:
        models = await llm.list_models(settings)
        logging.info("LLM server reachable", extra={"models": len(models)})
    except llm.LlmUnavailableError as exc:
        logging.warning(
            "LLM server unreachable at startup",
            extra={"base_url": settings.llm_base, "error": str(exc)[:200]},
        )


async def serve(settings: Settings) -> None:
    server = grpc.aio.server(maximum_concurrent_rpcs=settings.max_concurrent_analyses)
    pb_grpc.add_AnalysisServiceServicer_to_server(AnalysisServicer(), server)

    health_servicer = health.aio.HealthServicer()
    health_pb2_grpc.add_HealthServicer_to_server(health_servicer, server)
    for name in ("", SERVICE_NAME):
        await health_servicer.set(name, health_pb2.HealthCheckResponse.SERVING)

    server.add_insecure_port(settings.grpc_addr)
    await server.start()
    logging.info("ai service listening", extra={"addr": settings.grpc_addr})
    await check_llm(settings)

    stop = asyncio.Event()
    loop = asyncio.get_running_loop()
    for sig in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(sig, stop.set)
    await stop.wait()

    logging.info("shutting down")
    for name in ("", SERVICE_NAME):
        await health_servicer.set(name, health_pb2.HealthCheckResponse.NOT_SERVING)
    await server.stop(grace=10)


def main() -> None:
    try:
        settings = get_settings()
    except ValidationError as exc:
        print(f"invalid configuration:\n{exc}", file=sys.stderr)
        sys.exit(2)
    setup_logging(settings.log_level)
    asyncio.run(serve(settings))


if __name__ == "__main__":
    main()
