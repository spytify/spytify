# Behaviour gate (ci.yml). Stands in for the bug tracker during
# the scan: the app's reports land here and never leave the machine.
# Attachments are kept as their header and size only.
# Usage: sentry-sink.py <port> <out.jsonl>
import gzip
import json
import sys
import threading
import time
import zlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

KEEP = {"event", "transaction", "session", "sessions", "client_report", "check_in", "log",
        "span", "statsd", "metric_buckets", "feedback", "user_report"}
HEADERS = ("User-Agent", "Content-Type", "Content-Encoding", "X-Sentry-Auth")
lock = threading.Lock()


def items(body):
    """The envelope's header, then each item: its header, size and, for the
    kinds worth reading, its JSON."""
    nl = body.find(b"\n")
    try:
        out = [{"envelope": json.loads(body[:nl] if nl >= 0 else body)}]
    except Exception:
        return [{"raw_bytes": len(body)}]
    pos = nl + 1 if nl >= 0 else len(body)
    while pos < len(body):
        nl = body.find(b"\n", pos)
        line = body[pos:nl if nl >= 0 else len(body)]
        pos = nl + 1 if nl >= 0 else len(body)
        if not line.strip():
            continue
        try:
            head = json.loads(line)
        except Exception:
            out.append({"unparsed_line_bytes": len(line)})
            continue
        n = head.get("length")
        if n is None:
            nl = body.find(b"\n", pos)
            n = (nl if nl >= 0 else len(body)) - pos
        payload = body[pos:pos + n]
        pos += n + 1
        row = {"header": head, "bytes": len(payload)}
        if head.get("type") in KEEP:
            try:
                row["payload"] = json.loads(payload)
            except Exception:
                pass
        out.append(row)
    return out


class Sink(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def body(self):
        if self.headers.get("Transfer-Encoding", "").lower() == "chunked":
            data = b""
            while True:
                size = int(self.rfile.readline().strip() or b"0", 16)
                if size == 0:
                    self.rfile.readline()
                    break
                data += self.rfile.read(size)
                self.rfile.readline()
        else:
            data = self.rfile.read(int(self.headers.get("Content-Length") or 0))
        enc = self.headers.get("Content-Encoding", "").lower()
        if enc == "gzip":
            data = gzip.decompress(data)
        elif enc == "deflate":
            data = zlib.decompress(data)
        return data

    def handle_any(self):
        row = {"t": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "method": self.command,
               "path": self.path, "headers": {h: self.headers.get(h) for h in HEADERS if self.headers.get(h)}}
        try:
            data = self.body()
            row["bytes"] = len(data)
            row["items"] = items(data)
        except Exception as e:
            row["error"] = f"{type(e).__name__}: {e}"
        with lock, open(sys.argv[2], "a", encoding="utf-8") as f:
            f.write(json.dumps(row) + "\n")
        out = b'{"id":"00000000000000000000000000000000"}'
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(out)))
        self.end_headers()
        self.wfile.write(out)

    do_POST = do_PUT = do_GET = handle_any


if __name__ == "__main__":
    ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), Sink).serve_forever()
