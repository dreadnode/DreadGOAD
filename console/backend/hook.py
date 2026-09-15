"""Compatibility facade for post-command range synchronization."""

from .health_sync import apply_health
from .inventory_sync import apply_instances, run_check
from .topology_sync import reseed

__all__ = ["apply_health", "apply_instances", "reseed", "run_check"]
