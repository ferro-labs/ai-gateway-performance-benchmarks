#!/usr/bin/env python3
"""
Minimal OpenAI-compatible mock server for isolated gateway benchmarking.

Returns fixed, instant responses so that benchmark measurements reflect
gateway overhead only, not upstream LLM latency.

Usage:
    python scripts/mock_server.py [--port 9000] [--latency-ms 0]

Then set each gateway's upstream to http://localhost:9000 (or the container
hostname when using docker-compose).

Supports:
  - POST /v1/chat/completions  (blocking)
  - POST /v1/chat/completions  (streaming SSE, when stream=true in body)
  - GET  /health
"""

import argparse
import json
import time
import uuid
from http.server import BaseHTTPRequestHandler, HTTPServer
from typing import Optional

FIXED_CONTENT = "Gateway benchmark mock response."

_latency_ms: float = 0.0


def _make_completion(model: str, stream: bool = False) -> dict:
    return {
        "id": f"chatcmpl-{uuid.uuid4().hex[:12]}",
        "object": "chat.completion",
        "created": int(time.time()),
        "model": model,
        "choices": [
            {
                "index": 0,
                "message": {"role": "assistant", "content": FIXED_CONTENT},
                "finish_reason": "stop",
            }
        ],
        "usage": {"prompt_tokens": 10, "completion_tokens": 8, "total_tokens": 18},
    }


def _make_stream_chunks(model: str) -> list:
    """Return list of SSE data lines (as strings) for a minimal streaming response."""
    chunk_id = f"chatcmpl-{uuid.uuid4().hex[:12]}"
    words = FIXED_CONTENT.split()
    chunks = []

    # start chunk
    chunks.append(
        json.dumps(
            {
                "id": chunk_id,
                "object": "chat.completion.chunk",
                "created": int(time.time()),
                "model": model,
                "choices": [{"index": 0, "delta": {"role": "assistant", "content": ""}, "finish_reason": None}],
            }
        )
    )
    # content chunks
    for word in words:
        chunks.append(
            json.dumps(
                {
                    "id": chunk_id,
                    "object": "chat.completion.chunk",
                    "created": int(time.time()),
                    "model": model,
                    "choices": [{"index": 0, "delta": {"content": word + " "}, "finish_reason": None}],
                }
            )
        )
    # stop chunk
    chunks.append(
        json.dumps(
            {
                "id": chunk_id,
                "object": "chat.completion.chunk",
                "created": int(time.time()),
                "model": model,
                "choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}],
            }
        )
    )
    return chunks


class MockHandler(BaseHTTPRequestHandler):
    def log_message(self, format: str, *args) -> None:  # noqa: A002
        # Suppress per-request logs for cleaner benchmark output;
        # only errors will be printed.
        pass

    def _read_body(self) -> Optional[dict]:
        length = int(self.headers.get("Content-Length", 0))
        if length == 0:
            return None
        raw = self.rfile.read(length)
        try:
            return json.loads(raw)
        except json.JSONDecodeError:
            return None

    def do_GET(self) -> None:
        if self.path == "/health":
            self._send_json(200, {"status": "ok"})
        else:
            self._send_json(404, {"error": "not found"})

    def do_POST(self) -> None:
        if self.path not in ("/v1/chat/completions",):
            self._send_json(404, {"error": f"unknown path: {self.path}"})
            return

        body = self._read_body() or {}
        model = body.get("model", "mock-model")
        stream = body.get("stream", False)

        if _latency_ms > 0:
            time.sleep(_latency_ms / 1000.0)

        if stream:
            self._send_stream(model)
        else:
            self._send_json(200, _make_completion(model))

    def _send_json(self, status: int, data: dict) -> None:
        payload = json.dumps(data).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def _send_stream(self, model: str) -> None:
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.send_header("Transfer-Encoding", "chunked")
        self.end_headers()

        for chunk_data in _make_stream_chunks(model):
            line = f"data: {chunk_data}\n\n".encode("utf-8")
            self.wfile.write(line)
        self.wfile.write(b"data: [DONE]\n\n")
        self.wfile.flush()


def main() -> None:
    global _latency_ms  # noqa: PLW0603

    parser = argparse.ArgumentParser(description="OpenAI-compatible mock server for benchmarks")
    parser.add_argument("--port", type=int, default=9000, help="Port to listen on (default: 9000)")
    parser.add_argument(
        "--latency-ms",
        type=float,
        default=0.0,
        help="Artificial response delay in milliseconds (default: 0)",
    )
    args = parser.parse_args()
    _latency_ms = args.latency_ms

    server = HTTPServer(("0.0.0.0", args.port), MockHandler)
    print(f"Mock server listening on http://0.0.0.0:{args.port}  latency={_latency_ms}ms")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nShutting down.")


if __name__ == "__main__":
    main()
