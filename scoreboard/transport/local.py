"""Local file transport for development and testing."""

from pathlib import Path

from .base import Transport


class LocalTransport(Transport):
    """Read report.json from a local file path."""

    def __init__(self, path: str = "/tmp/report.jsonl"):
        self.path = Path(path)

    def fetch_report(self) -> str | None:
        if not self.path.exists():
            return None
        return self.path.read_text()

    def delete_report(self) -> bool:
        if not self.path.exists():
            return False
        self.path.unlink()
        return True
