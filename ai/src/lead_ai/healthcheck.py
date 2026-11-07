"""Container health check: exits 0 when the gRPC health service reports SERVING."""

from __future__ import annotations

import os
import sys

import grpc
from grpc_health.v1 import health_pb2, health_pb2_grpc


def main() -> int:
    port = os.environ.get("GRPC_ADDR", "0.0.0.0:50051").rsplit(":", 1)[-1]
    try:
        with grpc.insecure_channel(f"127.0.0.1:{port}") as channel:
            resp = health_pb2_grpc.HealthStub(channel).Check(
                health_pb2.HealthCheckRequest(service=""), timeout=2
            )
    except grpc.RpcError:
        return 1
    return 0 if resp.status == health_pb2.HealthCheckResponse.SERVING else 1


if __name__ == "__main__":
    sys.exit(main())
