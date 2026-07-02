import json
import os
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlparse

PORT = int(os.environ.get("PORT", "8080"))
MESSAGE = os.environ.get("MESSAGE", "hello from a Lambda MicroVM")
RUN_PAYLOAD_PATH = Path("/tmp/run-payload.txt")


class Handler(BaseHTTPRequestHandler):
    server_version = "unobin-microvm-demo/1.0"

    def do_GET(self):
        path = urlparse(self.path).path
        if path == "/health":
            self.send_json({"ok": True})
            return
        if path == "/":
            self.send_json({"message": MESSAGE, "run_payload": read_run_payload()})
            return
        self.send_json({"error": "not found", "path": path}, HTTPStatus.NOT_FOUND)

    def do_POST(self):
        path = urlparse(self.path).path
        body = self.rfile.read(int(self.headers.get("content-length", "0")))
        text = body.decode("utf-8", errors="replace")
        if path == "/run":
            RUN_PAYLOAD_PATH.write_text(text, encoding="utf-8")
        if path in {"/ready", "/validate", "/run", "/terminate"}:
            self.send_json({"ok": True, "hook": path.removeprefix("/"), "body": text})
            return
        self.send_json({"error": "not found", "path": path}, HTTPStatus.NOT_FOUND)

    def send_json(self, value, status=HTTPStatus.OK):
        data = json.dumps(value, sort_keys=True).encode("utf-8")
        self.send_response(status)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, format, *args):
        print(f"{self.address_string()} - {format % args}", flush=True)


def read_run_payload():
    try:
        return RUN_PAYLOAD_PATH.read_text(encoding="utf-8")
    except FileNotFoundError:
        return ""


if __name__ == "__main__":
    server = ThreadingHTTPServer(("0.0.0.0", PORT), Handler)
    print(f"listening on {PORT}", flush=True)
    server.serve_forever()
