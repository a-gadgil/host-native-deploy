#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

for svc_user in polyglot-go polyglot-rust; do
  if ! id -u "$svc_user" >/dev/null 2>&1; then
    sudo useradd --system --no-create-home --shell /usr/sbin/nologin "$svc_user"
  fi
done

sudo mkdir -p /opt/polyglot-rust-mariadb/go-api/releases \
              /opt/polyglot-rust-mariadb/rust-api/releases \
              /etc/polyglot-rust-mariadb

sudo chown -R polyglot-go:polyglot-go /opt/polyglot-rust-mariadb/go-api
sudo chown -R polyglot-rust:polyglot-rust /opt/polyglot-rust-mariadb/rust-api
sudo chmod 0750 /etc/polyglot-rust-mariadb

if [ ! -f /etc/polyglot-rust-mariadb/go-api.env ]; then
  sudo cp "$PROJECT_DIR/infra/env/go-api.env.example" /etc/polyglot-rust-mariadb/go-api.env
fi
sudo chown root:polyglot-go /etc/polyglot-rust-mariadb/go-api.env
sudo chmod 0640 /etc/polyglot-rust-mariadb/go-api.env

if [ ! -f /etc/polyglot-rust-mariadb/rust-api.env ]; then
  sudo cp "$PROJECT_DIR/infra/env/rust-api.env.example" /etc/polyglot-rust-mariadb/rust-api.env
fi
sudo chown root:polyglot-rust /etc/polyglot-rust-mariadb/rust-api.env
sudo chmod 0640 /etc/polyglot-rust-mariadb/rust-api.env

sudo cp "$PROJECT_DIR/infra/systemd/polyglot-rm-rust-api.service" /etc/systemd/system/
sudo cp "$PROJECT_DIR/infra/systemd/polyglot-rm-go-api.service" /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable polyglot-rm-rust-api polyglot-rm-go-api

sudo rm -f /etc/nginx/sites-enabled/*
sudo cp "$PROJECT_DIR/infra/nginx/polyglot-rust-mariadb.conf" /etc/nginx/sites-available/polyglot-rust-mariadb.conf
sudo ln -sfn /etc/nginx/sites-available/polyglot-rust-mariadb.conf /etc/nginx/sites-enabled/polyglot-rust-mariadb.conf
sudo nginx -t
sudo systemctl enable --now nginx
sudo systemctl reload nginx

echo "Bootstrap complete for polyglot-rust-mariadb."
