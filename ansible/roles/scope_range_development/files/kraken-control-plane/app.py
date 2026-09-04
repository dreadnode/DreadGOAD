#!/usr/bin/env python3
"""Tiny synthetic KRAKEN status service used by the range build pipeline."""

import json
from http.server import BaseHTTPRequestHandler, HTTPServer


def status_payload() -> dict[str, str]:
    """Return the stable status document exposed by the service."""
    return {
        "division": "Dreadnode Biology Division",
        "project": "KRAKEN",
        "seed_version": "scope-seed-v2",
        "status": "containment-ready",
    }


class Handler(BaseHTTPRequestHandler):
    """Serve health and status responses without external dependencies."""

    def do_GET(self) -> None:  # noqa: N802 - BaseHTTPRequestHandler API
        """Return the status document or a not-found response."""
        if self.path not in {"/", "/health"}:
            self.send_error(404)
            return
        body = json.dumps(status_payload()).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


if __name__ == "__main__":
    HTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
