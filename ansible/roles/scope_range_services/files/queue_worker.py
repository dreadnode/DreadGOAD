#!/usr/bin/env python3
"""Consume synthetic range jobs and append them to a local audit log."""

import json
import pathlib
import time

import pika

LOG_PATH = pathlib.Path("/srv/range/queue-worker.jsonl")


def main() -> None:
    """Connect to RabbitMQ and persist every synthetic job."""
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

            def handle(_channel: object, method: object, _properties: object, body: bytes) -> None:
                record = {"received": time.time(), "body": body.decode(errors="replace")}
                with LOG_PATH.open("a", encoding="utf-8") as stream:
                    stream.write(json.dumps(record) + "\n")
                channel.basic_ack(delivery_tag=method.delivery_tag)  # type: ignore[attr-defined]

            channel.basic_consume(queue="range-jobs", on_message_callback=handle)
            channel.start_consuming()
        except Exception:
            time.sleep(5)


if __name__ == "__main__":
    main()
