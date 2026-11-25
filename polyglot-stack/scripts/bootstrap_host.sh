#!/usr/bin/env bash
set -euo pipefail

STACK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

echo "[1/4] Creating unprivileged system users..."
for svc_user in polyglot-go polyglot-py; do
    if ! id -u "${svc_user}" >/dev/null 2>&1; then
        sudo useradd --system --no-create-home --shell /usr/sbin/nologin "${svc_user}"
    fi
done

echo "[2/4] Creating /opt release directories and /etc config directories..."
sudo mkdir -p /opt/polyglot-go-api/releases /etc/polyglot-go-api
sudo chown -R polyglot-go:polyglot-go /opt/polyglot-go-api
sudo chown root:polyglot-go /etc/polyglot-go-api
sudo chmod 0750 /etc/polyglot-go-api

sudo mkdir -p /opt/polyglot-python-api/releases /etc/polyglot-python-api
sudo chown -R polyglot-py:polyglot-py /opt/polyglot-python-api
sudo chown root:polyglot-py /etc/polyglot-python-api
sudo chmod 0750 /etc/polyglot-python-api

echo "[3/4] Installing environment files (preserving existing secrets if present)..."
if [ ! -f /etc/polyglot-go-api/go-api.env ]; then
    sudo install -o root -g polyglot-go -m 0640 \
        "${STACK_DIR}/infra/env/polyglot-go-api.env.example" \
        /etc/polyglot-go-api/go-api.env
fi

if [ ! -f /etc/polyglot-python-api/python-api.env ]; then
    sudo install -o root -g polyglot-py -m 0640 \
        "${STACK_DIR}/infra/env/polyglot-python-api.env.example" \
        /etc/polyglot-python-api/python-api.env
fi

echo "[4/4] Installing systemd units and Nginx reverse proxy config..."
sudo install -o root -g root -m 0644 \
    "${STACK_DIR}/infra/systemd/polyglot-python-api.service" \
    /etc/systemd/system/polyglot-python-api.service
sudo install -o root -g root -m 0644 \
    "${STACK_DIR}/infra/systemd/polyglot-go-api.service" \
    /etc/systemd/system/polyglot-go-api.service
sudo systemctl daemon-reload
sudo systemctl enable polyglot-python-api polyglot-go-api

sudo install -o root -g root -m 0644 \
    "${STACK_DIR}/infra/nginx/polyglot-stack.conf" \
    /etc/nginx/sites-available/polyglot-stack.conf
sudo rm -f /etc/nginx/sites-enabled/*
sudo ln -sfn /etc/nginx/sites-available/polyglot-stack.conf /etc/nginx/sites-enabled/polyglot-stack.conf
sudo nginx -t

echo "Bootstrap complete."
