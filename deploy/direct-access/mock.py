"""Cost-free streaming mock; never forwards to a model or production service."""
import hashlib
import json
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlsplit


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):
        # Request URLs can carry Gemini query credentials. Never log them.
        pass

    def handle_request(self):
        parsed = urlsplit(self.path)
        query = parse_qs(parsed.query)
        digest = hashlib.sha256()
        size = int(self.headers.get("Content-Length", "0"))
        remaining = size
        while remaining:
            chunk = self.rfile.read(min(1 << 20, remaining))
            if not chunk:
                self.send_error(400, "truncated upload")
                return
            digest.update(chunk)
            remaining -= len(chunk)
        mode = query.get("mode", ["echo"])[0]
        if mode == "delayed":
            time.sleep(130)
        if mode == "sse":
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Cache-Control", "no-cache")
            self.send_header("X-Accel-Buffering", "no")
            self.send_header("Connection", "close")
            self.end_headers()
            for index in range(14):
                self.wfile.write((": heartbeat %d\n\n" % index).encode())
                self.wfile.flush()
                if index != 13:
                    time.sleep(10)
            self.wfile.write(b'data: {"type":"response.completed","mock":true}\n\ndata: [DONE]\n\n')
            self.wfile.flush()
            self.close_connection = True
            return
        payload = {
            "mock": True,
            "method": self.command,
            "path": parsed.path,
            "bytes": size,
            "sha256": digest.hexdigest(),
            "headers": {
                name.lower(): self.headers.get(name, "")
                for name in (
                    "X-Forwarded-For", "X-Real-IP", "CF-Connecting-IP", "Forwarded",
                    "X-Original-URI", "X-Direct-Access-Authorized", "X-Direct-User-ID",
                )
            },
            "credential_present": any(self.headers.get(name) for name in
                                      ("Authorization", "X-Api-Key", "X-Goog-Api-Key"))
                or bool(query.get("key")),
        }
        body = json.dumps(payload).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    do_GET = handle_request
    do_POST = handle_request
    do_DELETE = handle_request
    do_OPTIONS = handle_request


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
