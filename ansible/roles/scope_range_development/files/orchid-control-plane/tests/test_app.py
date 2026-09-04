"""Tests for the synthetic ORCHID status service."""

import unittest

from app import status_payload


class StatusPayloadTests(unittest.TestCase):
    """Verify the seeded build exposes stable identifying data."""

    def test_status_payload(self) -> None:
        self.assertEqual(
            status_payload(),
            {
                "project": "ORCHID",
                "seed_version": "scope-seed-v1",
                "status": "ready",
            },
        )


if __name__ == "__main__":
    unittest.main()
