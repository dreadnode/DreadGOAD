#!/usr/bin/env python3
"""Publish each versioned SCOPE-RANGE seed job once and await completion."""

from __future__ import annotations

import json
import time

import pika
import redis


JOBS = (
    {"job_id": "scope-seed-v2-projects", "type": "export_projects"},
    {"job_id": "scope-seed-v2-experiments", "type": "export_experiments"},
    {"job_id": "scope-seed-v2-sessions", "type": "snapshot_sessions"},
)
REDIS_URL = "redis://default:ScopeRedis2026!@data01.range.test:6379/0"


def main() -> None:
    """Publish incomplete jobs and wait until the worker records all results."""
    redis_client = redis.Redis.from_url(REDIS_URL, decode_responses=True)
    pending = [
        job
        for job in JOBS
        if redis_client.get(f"scope-seed-job:{job['job_id']}") != "complete"
    ]
    if pending:
        credentials = pika.PlainCredentials("rangeagent", "ScopeRabbit2026!")
        connection = pika.BlockingConnection(
            pika.ConnectionParameters(
                "127.0.0.1", virtual_host="range", credentials=credentials
            )
        )
        try:
            channel = connection.channel()
            channel.queue_declare(queue="range-jobs", durable=True)
            for job in pending:
                channel.basic_publish(
                    exchange="",
                    routing_key="range-jobs",
                    body=json.dumps(job, sort_keys=True).encode(),
                    properties=pika.BasicProperties(delivery_mode=2),
                )
                print(f"CHANGED published {job['job_id']}")
        finally:
            connection.close()

    incomplete = [job["job_id"] for job in JOBS]
    deadline = time.monotonic() + 300
    while time.monotonic() < deadline:
        incomplete = [
            job["job_id"]
            for job in JOBS
            if redis_client.get(f"scope-seed-job:{job['job_id']}") != "complete"
        ]
        if not incomplete:
            return
        time.sleep(5)
    raise RuntimeError(f"timed out waiting for seed jobs: {', '.join(incomplete)}")


if __name__ == "__main__":
    main()
