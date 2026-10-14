#!/usr/bin/env python3
"""Run an isolated jobsdb + transformer + server against ingress_nonprod."""
import argparse
import datetime
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import urllib.error
import urllib.request


def run(*args, **kwargs):
    return subprocess.run(args, check=True, text=True, **kwargs)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--server-image", required=True)
    parser.add_argument("--transformer-image", required=True)
    parser.add_argument("--adc", type=Path, default=Path.home() / ".config/gcloud/application_default_credentials.json")
    parser.add_argument("--timeout", type=int, default=900)
    args = parser.parse_args()
    if not args.adc.is_file():
        parser.error("Application Default Credentials are required")

    run_id = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%d%H%M%S")
    event_name = f"precision_canary_{run_id}"
    expected = [9007199254740991, 9007199254740993, 9223372036854775807, -9223372036854775808]
    source_id, destination_id = f"precision-source-{run_id}", f"precision-destination-{run_id}"
    workspace = {
        "workspaceId": "precision-canary",
        "sources": [{
            "id": source_id, "workspaceId": "precision-canary", "name": "precision-canary", "enabled": True,
            "writeKey": "precision-canary", "config": {},
            "sourceDefinition": {"id": "precision", "name": "Java", "category": "", "type": "cloud"},
            "destinations": [{
                "id": destination_id, "workspaceId": "precision-canary", "name": "precision-canary", "enabled": True, "isProcessorEnabled": True,
                "destinationDefinition": {"id": "precision-bq", "name": "BQ", "displayName": "BigQuery", "category": "warehouse", "config": {"supportedSourceTypes": ["cloud"], "supportedConnectionModes": {"cloud": ["cloud"]}, "transformAtV1": "processor"}},
                "config": {
                    "connectionMode": "cloud", "project": "s555-data", "namespace": "ingress_nonprod", "location": "europe-north2",
                    "bucketName": "rudderstack-staging-nonprod", "credentials": "",
                    "prefix": f"precision-canary/{run_id}", "syncFrequency": "1",
                    "skipTracksTable": True, "skipUsersTable": True, "skipViews": True,
                },
                "transformations": [{"id": "precision-remove-ip", "versionId": "precision-remove-ip-v1", "enabled": True, "language": "javascript"}],
            }],
        }],
    }
    transformation = {
        "versionId": "precision-remove-ip-v1", "name": "Remove IP addresses", "codeVersion": "1",
        "code": "export function transformEvent(event) { if (event.context) { delete event.context.ip; delete event.context.request_ip; } delete event.context_ip; delete event.context_request_ip; delete event.request_ip; delete event.ip; return event; }",
    }

    cache = Path.home() / ".cache/rudderstack-precision-canary"
    cache.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="rudder-precision-", dir=cache) as directory:
        root = Path(directory)
        (root / "workspace.json").write_text(json.dumps(workspace))
        (root / "transformation.json").write_text(json.dumps(transformation))
        (root / "backend.py").write_text("""from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        body = Path('/config/transformation.json').read_bytes()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.end_headers()
        self.wfile.write(body)
HTTPServer(('0.0.0.0', 8080), Handler).serve_forever()
""")
        (root / "config.yaml").write_text("""enableStats: false
Processor:
  preserveIntegerPrecision: true
Warehouse:
  mode: embedded
workloadIdentity:
  type: GKE
""")
        compose = {"services": {
            "postgres": {"image": "postgres:17-alpine", "environment": {"POSTGRES_USER": "rudder", "POSTGRES_PASSWORD": "precision-canary", "POSTGRES_DB": "jobsdb"}, "healthcheck": {"test": ["CMD-SHELL", "pg_isready -U rudder -d jobsdb"], "interval": "2s", "timeout": "2s", "retries": 30}},
            "config-backend": {"image": "python:3.12-alpine", "command": ["python", "/config/backend.py"], "volumes": [f"{root}:/config:ro"]},
            "transformer": {"image": args.transformer_image, "platform": "linux/amd64", "environment": {"PRESERVE_INTEGER_PRECISION": "true", "CONFIG_BACKEND_URL": "http://config-backend:8080", "CLUSTER_ENABLED": "false", "LOG_LEVEL": "error"}, "depends_on": ["config-backend"]},
            "server": {"image": args.server_image, "platform": "linux/amd64", "ports": ["127.0.0.1:18089:8080"], "depends_on": {"postgres": {"condition": "service_healthy"}, "transformer": {"condition": "service_started"}}, "volumes": [f"{root}:/canary:ro", f"{args.adc.resolve()}:/gcp/adc.json:ro"], "environment": {
                "CONFIG_PATH": "/canary/config.yaml", "RSERVER_BACKEND_CONFIG_CONFIG_FROM_FILE": "true", "RSERVER_BACKEND_CONFIG_CONFIG_JSONPATH": "/canary/workspace.json",
                "CONFIG_BACKEND_TOKEN": "precision-canary", "DEST_TRANSFORM_URL": "http://transformer:9090",
                "JOBS_DB_HOST": "postgres", "JOBS_DB_PORT": "5432", "JOBS_DB_USER": "rudder", "JOBS_DB_PASSWORD": "precision-canary", "JOBS_DB_DB_NAME": "jobsdb",
                "GOOGLE_APPLICATION_CREDENTIALS": "/gcp/adc.json", "GOOGLE_CLOUD_PROJECT": "s555-data", "RUDDER_TMPDIR": "/tmp/rudderstack", "LOG_LEVEL": "INFO",
            }},
        }}
        compose_file = root / "compose.json"
        compose_file.write_text(json.dumps(compose))
        command = ["docker", "compose", "-p", f"precision-{run_id}", "-f", str(compose_file)]
        deadline = time.monotonic() + args.timeout
        try:
            run(*command, "up", "-d")
            while True:
                try:
                    urllib.request.urlopen("http://127.0.0.1:18089/health", timeout=5).close()
                    break
                except (urllib.error.URLError, ConnectionError):
                    if time.monotonic() >= deadline:
                        raise TimeoutError("canary gateway did not become ready")
                    time.sleep(3)
            timestamp = datetime.datetime.now(datetime.timezone.utc).isoformat()
            batch = [{"type": "track", "event": event_name, "messageId": f"{run_id}-{index}", "userId": "precision-canary", "timestamp": timestamp, "context": {"ip": "192.0.2.1"}, "properties": {"RoomOwnerPlayerId": value, "RunId": run_id, "Fraction": 1.25}} for index, value in enumerate(expected)]
            request = urllib.request.Request("http://127.0.0.1:18089/v1/batch", data=json.dumps({"batch": batch}).encode(), headers={"Content-Type": "application/json", "Authorization": "Basic cHJlY2lzaW9uLWNhbmFyeTo="})
            urllib.request.urlopen(request, timeout=30).close()
            print(f"Sent {len(expected)} synthetic events; table s555-data.ingress_nonprod.{event_name}", flush=True)
            query = f"SELECT room_owner_player_id FROM `s555-data.ingress_nonprod.{event_name}` WHERE run_id = '{run_id}' ORDER BY room_owner_player_id"
            while time.monotonic() < deadline:
                result = subprocess.run(["bq", "--project_id=s555-data", "--location=europe-north2", "query", "--use_legacy_sql=false", "--maximum_bytes_billed=104857600", "--format=json", query], text=True, capture_output=True)
                if result.returncode == 0:
                    rows = json.loads(result.stdout)
                    actual = [int(row["room_owner_player_id"]) for row in rows]
                    if len(actual) == len(expected):
                        if actual != sorted(expected):
                            raise AssertionError(f"integer mismatch: {actual!r}")
                        print(json.dumps({"runId": run_id, "table": event_name, "expected": sorted(expected), "actual": actual, "exact": True}), flush=True)
                        return
                time.sleep(15)
            raise TimeoutError("canary rows did not load before the timeout")
        finally:
            run(*command, "logs", "--no-color", "--tail=100", "server", "transformer")
            run(*command, "down", "--volumes")


if __name__ == "__main__":
    main()
