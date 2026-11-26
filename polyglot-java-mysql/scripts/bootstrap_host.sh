#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

echo "[1/5] Ensuring system users exist..."
for svc_user in polyglot-go polyglot-java; do
    if ! id -u "${svc_user}" >/dev/null 2>&1; then
        sudo useradd --system --no-create-home --shell /usr/sbin/nologin "${svc_user}"
    fi
done

echo "[2/5] Creating /opt/polyglot-java-mysql release directories..."
sudo install -d -o root -g root -m 0755 /opt/polyglot-java-mysql
sudo install -d -o polyglot-go -g polyglot-go -m 0755 /opt/polyglot-java-mysql/go-api/releases
sudo install -d -o polyglot-java -g polyglot-java -m 0755 /opt/polyglot-java-mysql/java-api/releases

echo "[3/5] Installing locked-down /etc/polyglot-java-mysql environment files..."
sudo install -d -o root -g root -m 0755 /etc/polyglot-java-mysql
if [ ! -f /etc/polyglot-java-mysql/go-api.env ]; then
    sudo install -o root -g polyglot-go -m 0640 "${PROJECT_DIR}/infra/env/go-api.env.example" /etc/polyglot-java-mysql/go-api.env
fi
if [ ! -f /etc/polyglot-java-mysql/java-api.env ]; then
    sudo install -o root -g polyglot-java -m 0640 "${PROJECT_DIR}/infra/env/java-api.env.example" /etc/polyglot-java-mysql/java-api.env
fi

echo "[4/5] Installing systemd unit files..."
sudo install -o root -g root -m 0644 "${PROJECT_DIR}/infra/systemd/polyglot-jm-go-api.service" /etc/systemd/system/polyglot-jm-go-api.service
sudo install -o root -g root -m 0644 "${PROJECT_DIR}/infra/systemd/polyglot-jm-java-api.service" /etc/systemd/system/polyglot-jm-java-api.service
sudo systemctl daemon-reload
sudo systemctl enable polyglot-jm-go-api polyglot-jm-java-api

echo "[5/5] Configuring Nginx reverse proxy..."
sudo rm -f /etc/nginx/sites-enabled/default /etc/nginx/sites-enabled/polyglot-stack.conf
sudo install -o root -g root -m 0644 "${PROJECT_DIR}/infra/nginx/polyglot-java-mysql.conf" /etc/nginx/sites-available/polyglot-java-mysql.conf
sudo ln -sfn /etc/nginx/sites-available/polyglot-java-mysql.conf /etc/nginx/sites-enabled/polyglot-java-mysql.conf
sudo nginx -t
sudo systemctl enable --now nginx
sudo systemctl reload nginx

echo "Bootstrap complete."
