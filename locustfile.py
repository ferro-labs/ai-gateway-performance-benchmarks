import json
import os
import time
from typing import Any, Dict

from locust import HttpUser, between, task

REQUEST_PATH = os.getenv("REQUEST_PATH", "/v1/chat/completions")
MODEL = os.getenv("MODEL", "gpt-4o-mini")
API_KEY = os.getenv("API_KEY", "")
PROMPT = os.getenv("PROMPT", "Return exactly one short sentence about load testing.")
MAX_TOKENS = int(os.getenv("MAX_TOKENS", "64"))
TEMPERATURE = float(os.getenv("TEMPERATURE", "0"))
EXTRA_HEADERS_RAW = os.getenv("EXTRA_HEADERS", "")
STREAM = os.getenv("STREAM", "false").lower() in ("1", "true", "yes")
WAIT_TIME_MIN = float(os.getenv("WAIT_TIME_MIN", "0"))
WAIT_TIME_MAX = float(os.getenv("WAIT_TIME_MAX", "0"))


def parse_extra_headers(raw: str) -> Dict[str, str]:
    if not raw:
        return {}
    try:
        parsed = json.loads(raw)
        if isinstance(parsed, dict):
            return {str(k): str(v) for k, v in parsed.items()}
    except json.JSONDecodeError:
        pass
    return {}


class ChatCompletionUser(HttpUser):
    wait_time = between(WAIT_TIME_MIN, WAIT_TIME_MAX)

    def on_start(self) -> None:
        headers: Dict[str, str] = {"Content-Type": "application/json"}
        if API_KEY:
            headers["Authorization"] = f"Bearer {API_KEY}"
        headers.update(parse_extra_headers(EXTRA_HEADERS_RAW))
        self.headers = headers

    @task
    def chat_completion(self) -> None:
        if STREAM:
            self._chat_streaming()
        else:
            self._chat_blocking()

    def _chat_blocking(self) -> None:
        payload: Dict[str, Any] = {
            "model": MODEL,
            "messages": [{"role": "user", "content": PROMPT}],
            "max_tokens": MAX_TOKENS,
            "temperature": TEMPERATURE,
        }

        with self.client.post(
            REQUEST_PATH,
            headers=self.headers,
            json=payload,
            name="chat.completions",
            catch_response=True,
        ) as response:
            if response.status_code != 200:
                response.failure(f"HTTP {response.status_code}: {response.text[:200]}")
                return

            try:
                body = response.json()
            except ValueError:
                response.failure("Invalid JSON response")
                return

            if not isinstance(body, dict) or "choices" not in body:
                response.failure("Missing 'choices' in response body")

    def _chat_streaming(self) -> None:
        """
        SSE streaming request. Measures full stream duration (time-to-last-token).
        Locust records this as a single request with the total elapsed time.
        """
        payload: Dict[str, Any] = {
            "model": MODEL,
            "messages": [{"role": "user", "content": PROMPT}],
            "max_tokens": MAX_TOKENS,
            "temperature": TEMPERATURE,
            "stream": True,
        }

        start = time.monotonic()
        request_name = "chat.completions.stream"

        try:
            with self.client.post(
                REQUEST_PATH,
                headers=self.headers,
                json=payload,
                name=request_name,
                catch_response=True,
                stream=True,
            ) as response:
                if response.status_code != 200:
                    response.failure(f"HTTP {response.status_code}: {response.text[:200]}")
                    return

                received_chunks = 0
                saw_done = False
                for raw_line in response.iter_lines():
                    if not raw_line:
                        continue
                    line = raw_line if isinstance(raw_line, str) else raw_line.decode("utf-8", errors="replace")
                    if line.startswith("data: "):
                        data = line[6:].strip()
                        if data == "[DONE]":
                            saw_done = True
                            break
                        try:
                            chunk = json.loads(data)
                            if isinstance(chunk, dict) and "choices" in chunk:
                                received_chunks += 1
                        except json.JSONDecodeError:
                            pass

                elapsed_ms = (time.monotonic() - start) * 1000

                if received_chunks == 0 and not saw_done:
                    response.failure("No SSE chunks received")
                    return

                # Manually report total stream duration as the response time
                self.environment.events.request.fire(
                    request_type="POST",
                    name=request_name + ".ttlt",
                    response_time=elapsed_ms,
                    response_length=0,
                    exception=None,
                    context={},
                )

        except Exception as exc:  # pylint: disable=broad-except
            elapsed_ms = (time.monotonic() - start) * 1000
            self.environment.events.request.fire(
                request_type="POST",
                name=request_name,
                response_time=elapsed_ms,
                response_length=0,
                exception=exc,
                context={},
            )
