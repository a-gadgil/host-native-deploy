# Polyglot Rust-MariaDB Stack (Host-Native 3-Tier Architecture)

Host-native 3-tier Order & Risk-Scoring stack built with Nginx, Go (`net/http` + `go-sql-driver/mysql`), Rust (`axum` + `tokio` + `sqlx`), Redis 7, and MariaDB 11.

## Architecture & Runtime Layout

| Tier | Service | Stack & Endpoint | System User / Isolation |
| :--- | :--- | :--- | :--- |
| Tier 1 (Edge) | `nginx` | Reverse proxy (`0.0.0.0:80`) | `www-data`, blocks `/internal/*` (`403`) |
| Tier 2A (Core API) | `go-api` | Go `1.23` (`127.0.0.1:8090`) | `polyglot-go` (`nologin`), `ProtectSystem=strict` |
| Tier 2B (Compute) | `rust-api` | Rust `axum` + `sqlx` (`127.0.0.1:8091`) | `polyglot-rust` (`nologin`), `ProtectSystem=strict` |
| Tier 3A (Record Store) | `mariadb` | MariaDB (`127.0.0.1:3306`, `polyglot_mariadb_db`) | `mysql`, least-privilege `polyglot_app` user |
| Tier 3B (Cache) | `redis` | Redis 7 (`127.0.0.1:6379`) | `redis`, cache-aside (`order:{id}`, `analytics:summary`) |

## Lifecycle Automation

1. **Idempotent Host Provisioning:**
   `./scripts/bootstrap_host.sh`
2. **Atomic Release Build & Symlink Deployment:**
   `./scripts/deploy_release.sh v1.0.0`
