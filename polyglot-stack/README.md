# Polyglot Stack (Host-Native 3-Tier Architecture)

5-component polyglot Order & Risk-Scoring stack deployed natively on Linux using unprivileged system users, atomic /opt release symlinks, /etc environment files, hardened systemd units, and an Nginx edge reverse proxy.

## Architecture & Port Contract

| Tier | Component | Runtime / Engine | Bind Address | System User |
| :--- | :--- | :--- | :--- | :--- |
| Tier 1 (Edge) | nginx | Reverse Proxy | 0.0.0.0:80 | www-data |
| Tier 2A (Core API) | go-api | Go (net/http) | 127.0.0.1:8090 | polyglot-go |
| Tier 2B (Compute) | python-api | FastAPI + Uvicorn | 127.0.0.1:8091 | polyglot-py |
| Tier 3A (Record Store) | postgresql | PostgreSQL 16 | 127.0.0.1:5432 | postgres |
| Tier 3B (Cache) | redis-server | Redis 7 | 127.0.0.1:6379 | redis |

## Lifecycle Commands

1. Provision OS users, /opt & /etc directories, systemd units, and Nginx config:
   ./scripts/bootstrap_host.sh

2. Build artifacts, flip atomic symlinks, and restart services:
   ./scripts/deploy_release.sh v1.0.0
