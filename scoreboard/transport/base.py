"""Base transport interface for reading the agent's report file."""

from abc import ABC, abstractmethod


class Transport(ABC):
    """Abstract base for fetching report.json from the agent's environment."""

    @abstractmethod
    def fetch_report(self) -> str | None:
        """Fetch the raw JSON string of the report file.

        Returns None if the file doesn't exist yet or can't be read.
        """
        ...

    @abstractmethod
    def delete_report(self) -> bool:
        """Delete the report file. Returns True if deleted, False if not found."""
        ...
