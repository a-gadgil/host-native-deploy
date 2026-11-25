#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-v1.0.0}"
STACK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

echo "=== Deploying polyglot-stack release: ${VERSION} ==="

# 1. Build & stage Python release
PY_REL="/opt/polyglot-python-api/releases/${VERSION}"
echo "[1/4] Staging Python release at ${PY_REL}..."
sudo rm -rf "${PY_REL}"
sudo mkdir -p "${PY_REL}/app"
sudo cp "${STACK_DIR}/python-api/requirements.txt" "${PY_REL}/"
sudo cp "${STACK_DIR}/python-api/app/"*.py "${PY_REL}/app/"
sudo chown -R polyglot-py:polyglot-py "${PY_REL}"
sudo -u polyglot-py python3 -m venv "${PY_REL}/.venv"
sudo -u polyglot-py "${PY_REL}/.venv/bin/pip" install -q --upgrade pip
sudo -u polyglot-py "${PY_REL}/.venv/bin/pip" install -q -r "${PY_REL}/requirements.txt"

# 2. Build & stage Go binary
GO_REL="/opt/polyglot-go-api/releases/${VERSION}"
TMP_BIN="/tmp/polyglot-go-api-${VERSION}"
echo "[2/4] Building & staging Go release at ${GO_REL}..."
(
    cd "${STACK_DIR}/go-api"
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "${TMP_BIN}" ./cmd/server
)
sudo rm -rf "${GO_REL}"
sudo mkdir -p "${GO_REL}"
sudo install -o polyglot-go -g polyglot-go -m 0755 "${TMP_BIN}" "${GO_REL}/go-api"
rm -f "${TMP_BIN}"

# 3. Flip atomic symlinks
echo "[3/4] Flipping atomic 'current' symlinks..."
sudo -u polyglot-py ln -sfn "${PY_REL}" /opt/polyglot-python-api/current
sudo -u polyglot-go ln -sfn "${GO_REL}" /opt/polyglot-go-api/current

# 4. Restart services in dependency order & reload Nginx
echo "[4/4] Restarting systemd units and reloading Nginx..."
sudo systemctl restart polyglot-python-api
sudo systemctl restart polyglot-go-api
sudo systemctl enable --now nginx
sudo systemctl reload nginx

echo "=== Release ${VERSION} deployed successfully ==="
