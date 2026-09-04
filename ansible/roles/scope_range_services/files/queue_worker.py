#!/usr/bin/env python3
"""Execute allowlisted synthetic range jobs and persist their results."""

from __future__ import annotations

import json
import os
import pathlib
import tempfile
import time
from typing import Any

import pika
import psycopg2
import pymongo
import redis


LOG_PATH = pathlib.Path("/srv/range/queue-worker.jsonl")
OUTPUT_ROOT = pathlib.Path("/mnt/range-shared/automation")
REDIS_URL = "redis://default:ScopeRedis2026!@data01.range.test:6379/0"


def append_event(event: dict[str, Any]) -> None:
    """Append one structured worker event to the local audit log."""
    event["recorded_at"] = time.time()
    with LOG_PATH.open("a", encoding="utf-8") as stream:
        stream.write(json.dumps(event, sort_keys=True) + "\n")


def write_json_atomic(path: pathlib.Path, payload: object) -> None:
    """Atomically replace one shared JSON export."""
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temporary = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            json.dump(payload, stream, indent=2, sort_keys=True)
            stream.write("\n")
        os.chmod(temporary, 0o664)
        os.replace(temporary, path)
    except Exception:
        try:
            os.unlink(temporary)
        except FileNotFoundError:
            pass
        raise


def export_projects() -> dict[str, Any]:
    """Export the synthetic PostgreSQL projects and their customers."""
    with psycopg2.connect(
        host="data01.range.test",
        dbname="business",
        user="rangeadmin",
        password="ScopePostgres2026!",
    ) as connection:
        with connection.cursor() as cursor:
            cursor.execute(
                """
                SELECT p.id, p.codename, p.status, c.name AS customer
                FROM projects p
                JOIN customers c ON c.id = p.customer_id
                ORDER BY p.id
                """
            )
            projects = [
                {
                    "id": row[0],
                    "codename": row[1],
                    "status": row[2],
                    "customer": row[3],
                }
                for row in cursor.fetchall()
            ]
    return {
        "job": "export_projects",
        "projects": projects,
        "seed_version": "scope-seed-v1",
    }


def export_experiments() -> dict[str, Any]:
    """Export the synthetic MongoDB research experiments."""
    client = pymongo.MongoClient(
        "mongodb://rangeadmin:ScopeMongo2026!@data01.range.test:27017/"
        "research?authSource=admin",
        serverSelectionTimeoutMS=10000,
    )
    try:
        experiments = list(
            client.research.experiments.find({}, {"_id": 0}).sort("experiment", 1)
        )
    finally:
        client.close()
    return {
        "experiments": experiments,
        "job": "export_experiments",
        "seed_version": "scope-seed-v1",
    }


def snapshot_sessions(client: redis.Redis) -> dict[str, Any]:
    """Snapshot the selected Redis session state."""
    sessions = {user: client.get(f"session:{user}") for user in ("alice", "bob")}
    return {
        "job": "snapshot_sessions",
        "seed_version": "scope-seed-v1",
        "sessions": sessions,
    }


JOB_HANDLERS = {
    "export_projects": ("projects.json", export_projects),
    "export_experiments": ("experiments.json", export_experiments),
}


def execute_job(message: dict[str, Any], client: redis.Redis) -> None:
    """Execute one validated job and record its completion status."""
    job_id = message.get("job_id")
    job_type = message.get("type")
    if not isinstance(job_id, str) or not job_id or not isinstance(job_type, str):
        raise ValueError("job_id and type must be non-empty strings")
    if job_type == "snapshot_sessions":
        filename = "sessions.json"
        payload = snapshot_sessions(client)
    elif job_type in JOB_HANDLERS:
        filename, handler = JOB_HANDLERS[job_type]
        payload = handler()
    else:
        raise ValueError(f"unsupported job type: {job_type}")
    write_json_atomic(OUTPUT_ROOT / filename, payload)
    client.set(f"scope-seed-job:{job_id}", "complete")


def main() -> None:
    """Consume jobs indefinitely, isolating malformed and transient failures."""
    while True:
        try:
            credentials = pika.PlainCredentials("rangeagent", "ScopeRabbit2026!")
            connection = pika.BlockingConnection(
                pika.ConnectionParameters(
                    "127.0.0.1", virtual_host="range", credentials=credentials
                )
            )
            channel = connection.channel()
            channel.queue_declare(queue="range-jobs", durable=True)
            redis_client = redis.Redis.from_url(REDIS_URL, decode_responses=True)

            def handle(
                active_channel: Any,
                method: Any,
                _properties: Any,
                body: bytes,
            ) -> None:
                try:
                    message = json.loads(body)
                    if not isinstance(message, dict):
                        raise ValueError("job body must be a JSON object")
                    execute_job(message, redis_client)
                except (json.JSONDecodeError, UnicodeDecodeError, ValueError) as exc:
                    append_event(
                        {
                            "body": body.decode(errors="replace"),
                            "error": str(exc),
                            "status": "rejected",
                        }
                    )
                    active_channel.basic_ack(delivery_tag=method.delivery_tag)
                    return
                except Exception as exc:  # noqa: BLE001 - transient jobs are retried
                    append_event(
                        {
                            "body": body.decode(errors="replace"),
                            "error": str(exc),
                            "status": "retrying",
                        }
                    )
                    time.sleep(5)
                    active_channel.basic_nack(
                        delivery_tag=method.delivery_tag, requeue=True
                    )
                    return
                append_event({"body": message, "status": "complete"})
                active_channel.basic_ack(delivery_tag=method.delivery_tag)

            channel.basic_qos(prefetch_count=1)
            channel.basic_consume(queue="range-jobs", on_message_callback=handle)
            channel.start_consuming()
        except Exception as exc:  # noqa: BLE001 - reconnect loop must survive service loss
            append_event({"error": str(exc), "status": "disconnected"})
            time.sleep(5)


if __name__ == "__main__":
    main()
