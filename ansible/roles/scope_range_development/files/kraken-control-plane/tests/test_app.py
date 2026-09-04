"""Tests for the synthetic KRAKEN status service."""

import unittest

from app import status_payload


class StatusPayloadTests(unittest.TestCase):
    """Verify the seeded build exposes stable identifying data."""

    def test_status_payload(self) -> None:
        self.assertEqual(
            status_payload(),
            {
                "division": "Dreadnode Biology Division",
                "project": "KRAKEN",
                "seed_version": "scope-seed-v2",
                "status": "containment-ready",
            },
        )


if __name__ == "__main__":
    unittest.main()
