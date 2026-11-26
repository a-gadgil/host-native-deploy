#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-v1.0.0}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

GO_REL_DIR="/opt/polyglot-java-mysql/go-api/releases/${VERSION}"
JAVA_REL_DIR="/opt/polyglot-java-mysql/java-api/releases/${VERSION}"

echo "[1/5] Building Go static binary (${VERSION})..."
TMP_GO_BIN="$(mktemp /tmp/polyglot-jm-go-api.XXXXXX)"
(
    cd "${PROJECT_DIR}/go-api"
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "${TMP_GO_BIN}" ./cmd/server
)
sudo install -d -o polyglot-go -g polyglot-go -m 0755 "${GO_REL_DIR}"
sudo install -o polyglot-go -g polyglot-go -m 0755 "${TMP_GO_BIN}" "${GO_REL_DIR}/go-api"
rm -f "${TMP_GO_BIN}"

echo "[2/5] Building Java 21 Spring Boot fat JAR (${VERSION})..."
(
    cd "${PROJECT_DIR}/java-api"
    mvn -q clean package -DskipTests
)
sudo install -d -o polyglot-java -g polyglot-java -m 0755 "${JAVA_REL_DIR}"
sudo install -o polyglot-java -g polyglot-java -m 0644 "${PROJECT_DIR}/java-api/target/java-api.jar" "${JAVA_REL_DIR}/java-api.jar"

echo "[3/5] Flipping atomic 'current' symlinks..."
sudo ln -sfn "${GO_REL_DIR}" /opt/polyglot-java-mysql/go-api/current
sudo ln -sfn "${JAVA_REL_DIR}" /opt/polyglot-java-mysql/java-api/current
sudo chown -h polyglot-go:polyglot-go /opt/polyglot-java-mysql/go-api/current
sudo chown -h polyglot-java:polyglot-java /opt/polyglot-java-mysql/java-api/current

echo "[4/5] Restarting systemd services..."
sudo systemctl restart polyglot-jm-java-api
sudo systemctl restart polyglot-jm-go-api
sudo systemctl reload nginx

echo "[5/5] Waiting for JVM + Go readiness probes..."
for i in $(seq 1 15); do
    if curl -fsS http://127.0.0.1/api/v1/java/readyz >/dev/null 2>&1 && \
       curl -fsS http://127.0.0.1/api/v1/go/readyz >/dev/null 2>&1; then
        echo "Release ${VERSION} is LIVE and READY."
        exit 0
    fi
    sleep 1
done

echo "ERROR: Readiness probes did not pass within 15s." >&2
sudo systemctl status polyglot-jm-java-api polyglot-jm-go-api --no-pager
exit 1
