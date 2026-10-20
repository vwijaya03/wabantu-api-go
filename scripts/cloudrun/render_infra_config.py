#!/usr/bin/env python3
"""Render deploy/cloudrun/infra.config.json from env. No secret values are written."""

import json
import os
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PUBSUB_PATH = ROOT / "deploy" / "cloudrun" / "pubsub.json"
OUT_PATH = ROOT / "deploy" / "cloudrun" / "infra.config.json"

HOST_RE = re.compile(r"^[a-zA-Z0-9.-]+(:[0-9]+)?$")
PLACEHOLDER_URL = "https://pending.invalid"

# Encore secret names declared in code. Values come from process env at runtime.
ENCORE_SECRETS = [
    "JWTSecret",
    "DataEncryptionKey",
    "RedisURL",
    "PlatformAdminBootstrapKey",
    "WebhookVerifyToken",
    "AnthropicAPIKey",
    "AiInternalToken",
    "GitHubActionsToken",
    "AiTriageInternalToken",
    "MidtransServerKey",
    "MidtransClientKey",
    "MidtransIsProduction",
    "OpenAIApiKey",
    "PineconeApiKey",
    "PineconeIndexHost",
    "RetrievalBudgetMs",
    "RajaOngkirAPIKey",
    "RajaOngkirAccountType",
    "AWSS3Bucket",
    "AWSS3Region",
    "AWSS3AccessKeyID",
    "AWSS3SecretAccessKey",
    "SentryDSN",
]


def need(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        sys.exit(f"missing {name}")
    return value


def host(name: str) -> str:
    value = need(name)
    if not HOST_RE.match(value):
        sys.exit(f"{name} must be hostname or hostname:port, got a value that does not match")
    return value


def main() -> None:
    project = need("GCP_PROJECT")
    sql_host = host("SQL_HOST")
    sql_user = need("SQL_USER")
    system_db = os.environ.get("SQL_SYSTEM_DB", "system").strip() or "system"
    tenant_db = os.environ.get("SQL_TENANT_DB", "tenant").strip() or "tenant"
    redis_host = host("REDIS_HOST")
    base_url = os.environ.get("CLOUD_RUN_URL", "").strip().rstrip("/") or PLACEHOLDER_URL
    sa = os.environ.get("RUNTIME_SA", "wabantu-cloudrun").strip()
    sa_email = f"{sa}@{project}.iam.gserviceaccount.com"

    topics = {}
    for row in json.loads(PUBSUB_PATH.read_text()):
        topic = row["topic"]
        sub = row.get("subscription")
        entry = {
            "name": f"lomba-{topic}",
            "project_id": project,
            "subscriptions": {},
        }
        if sub:
            entry["subscriptions"][sub] = {
                "name": f"lomba-{sub}",
                "project_id": project,
                "push_config": {
                    "id": sub,
                    "service_account": sa_email,
                    "jwt_audience": "-",  # must match the subscription audience; see deploy.sh comment
                },
            }
        topics[topic] = entry

    cfg = {
        "$schema": "https://encore.dev/schemas/infra.schema.json",
        "metadata": {
            "app_id": "wabantu-viko-8vni",
            "env_name": "lomba",
            "env_type": "production",
            "cloud": "gcp",
            "base_url": base_url,
        },
        "graceful_shutdown": {"total": 10, "shutdown_hooks": 3, "handlers": 7},
        "auth": [
            {
                "type": "key",
                "id": 1,
                "key": {"$env": "ENCORE_SERVICE_AUTH_KEY"},
            }
        ],
        "sql_servers": [
            {
                "host": sql_host,
                "tls_config": {"disabled": False},
                "databases": {
                    "system": {
                        "name": system_db,
                        "username": sql_user,
                        "password": {"$env": "SQL_PASSWORD"},
                        "max_connections": 10,
                        "min_connections": 1,
                    },
                    "tenant": {
                        "name": tenant_db,
                        "username": sql_user,
                        "password": {"$env": "SQL_PASSWORD"},
                        "max_connections": 10,
                        "min_connections": 1,
                    },
                },
            }
        ],
        "redis": {
            "ai-sessions": {
                "host": redis_host,
                "database_index": 0,
                "key_prefix": "lomba:",
                "max_connections": 10,
                "min_connections": 1,
                "tls_config": {"disabled": False},
                "auth": {
                    "type": "auth_string",
                    "auth_string": {"$env": "REDIS_AUTH"},
                },
            }
        },
        "secrets": {name: {"$env": name} for name in ENCORE_SECRETS},
        "pubsub": [
            {
                "type": "gcp_pubsub",
                "project_id": project,
                "topics": topics,
            }
        ],
    }

    OUT_PATH.write_text(json.dumps(cfg, indent=2) + "\n")
    print(f"wrote {OUT_PATH.relative_to(ROOT)} base_url={base_url}")


if __name__ == "__main__":
    main()
