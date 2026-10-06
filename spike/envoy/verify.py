#!/usr/bin/env python3
"""Build, run and qualify the real Envoy module; clean up only this run's resources."""
import concurrent.futures
import http.client
import json
import os
from pathlib import Path
import subprocess
import time

DIRECTORY = Path(__file__).resolve().parent
PROJECT = f"fig-match-{os.getpid()}"
COMPOSE = ["docker", "compose", "-p", PROJECT, "-f", str(DIRECTORY / "compose.yaml")]


def compose(*args):
    return subprocess.check_output(COMPOSE + list(args), text=True).strip()


def address(service, port):
    host, mapped = compose("port", service, str(port)).rsplit(":", 1)
    return host, int(mapped)


def request(endpoint, path, body=None, headers=None):
    connection = http.client.HTTPConnection(*endpoint, timeout=10)
    try:
        connection.request("POST" if body is not None else "GET", path, body, headers or {})
        response = connection.getresponse()
        return response.status, response.read()
    finally:
        connection.close()


def require(condition, message):
    if not condition:
        raise AssertionError(message)


def main():
    try:
        subprocess.run(COMPOSE + ["up", "--build", "-d"], check=True)
        proxy, admin, backend = address("proxy", 10000), address("proxy", 9901), address("echo", 8080)
        deadline = time.monotonic() + 30
        while True:
            try:
                if request(admin, "/ready")[0] == 200 and request(backend, "/count")[0] == 200:
                    break
            except OSError:
                pass
            if time.monotonic() > deadline:
                raise RuntimeError("Envoy/backend readiness timeout")
            time.sleep(0.2)

        def count():
            return json.loads(request(backend, "/count")[1])["received"]

        def allowed(path, body, expected_policy, expected_plan):
            before = count()
            status, raw = request(proxy, path, body, {"content-type": "application/json",
                                 "x-fig-policy": "spoofed", "x-fig-plan": "spoofed"})
            require(status == 200, f"allowed request failed: {status} {raw!r}")
            result = json.loads(raw)
            require(result["policy"] == expected_policy and result["plan"] == expected_plan,
                    f"incorrect handoff: {result}")
            require(result["body"] == (body or ""), "forwarded body changed")
            require(count() == before + 1, "backend did not receive exactly one request")

        allowed("/headers", None, "baseline", "")
        allowed("/chat", '{"model":"support-chat"}', "chat-policy", "support")
        print("PASS native header/body Match, default, spoof replacement and unchanged forwarding", flush=True)

        denied = [
            (404, '{"model":"unknown"}', {}),
            (404, '{}', {}),
            (400, '{', {}),
            (400, '{"model":null}', {}),
            (400, '{"model":42}', {}),
            (400, '{"model":"support-chat","model":"unknown"}', {}),
            (400, '{"model":"support-chat","nested":' + '[' * 33 + '0' + ']' * 33 + '}', {}),
            (400, '', {}),
            (413, 'x' * 4097, {}),
            (415, '{}', {"content-type": "text/plain"}),
            (415, '{}', {"content-encoding": "gzip"}),
        ]
        for expected, body, extra in denied:
            before = count()
            status, raw = request(proxy, "/chat", body, {"content-type": "application/json", **extra})
            require(status == expected, f"expected {expected}, got {status}: {raw!r}")
            require(count() == before, f"denied request reached backend: {status}")
        print(f"PASS {len(denied)} denial cases with no backend receipt", flush=True)

        def chunked(parts, trailers=False, expected=200):
            before = count()
            conn = http.client.HTTPConnection(*proxy, timeout=10)
            try:
                conn.putrequest("POST", "/chat")
                conn.putheader("content-type", "application/json")
                conn.putheader("transfer-encoding", "chunked")
                if trailers:
                    conn.putheader("trailer", "x-finish")
                conn.endheaders()
                for part in parts:
                    conn.send(f"{len(part):x}\r\n".encode() + part + b"\r\n")
                    if expected == 200:
                        time.sleep(0.1)
                        require(count() == before, "dispatched before complete body")
                conn.send(b"0\r\nx-finish: yes\r\n\r\n" if trailers else b"0\r\n\r\n")
                response = conn.getresponse()
                raw = response.read()
                require(response.status == expected, f"chunked status {response.status}: {raw!r}")
                if expected == 200:
                    result = json.loads(raw)
                    require(result["plan"] == "support", "chunked selection missing")
                    require(result["body"].encode() == b"".join(parts), "chunked body duplicated or lost")
                require(count() == before + (expected == 200), "chunked backend count mismatch")
            finally:
                conn.close()

        chunks = [b'{"model":', b'"support-', b'chat"}']
        chunked(chunks)
        chunked(chunks, trailers=True)
        chunked([b' ' * 2048, b' ' * 2049], expected=413)
        print("PASS fragmented bodies, trailers, pending dispatch gate and streaming size limit", flush=True)

        before = count()
        conn = http.client.HTTPConnection(*proxy, timeout=10)
        conn.putrequest("POST", "/chat")
        conn.putheader("content-type", "application/json")
        conn.putheader("content-length", "100")
        conn.endheaders()
        conn.send(b'{"model":')
        conn.close()
        time.sleep(0.2)
        require(count() == before, "aborted request reached backend")
        allowed("/chat", '{"model":"support-chat"}', "chat-policy", "support")
        print("PASS client abort before selection and subsequent request recovery", flush=True)

        before = count()
        def parallel(i):
            body = json.dumps({"model": "support-chat", "id": i})
            status, raw = request(proxy, "/chat", body, {"content-type": "application/json"})
            require(status == 200, f"parallel status {status}")
            result = json.loads(raw)
            require(result["body"] == body and result["plan"] == "support", "cross-request state leak")
        with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
            list(pool.map(parallel, range(40)))
        require(count() == before + 40, "parallel receipt mismatch")
        print("PASS 40 concurrent selections on two Envoy workers", flush=True)
        info = json.loads(request(admin, "/server_info")[1])
        print(json.dumps({"envoy": info["version"], "project": PROJECT, "backend_received": count()}), flush=True)
    except BaseException:
        subprocess.run(COMPOSE + ["logs", "--no-color", "--tail", "100"], check=False)
        raise
    finally:
        subprocess.run(COMPOSE + ["down", "--volumes", "--remove-orphans", "--rmi", "local"], check=True)


if __name__ == "__main__":
    main()
