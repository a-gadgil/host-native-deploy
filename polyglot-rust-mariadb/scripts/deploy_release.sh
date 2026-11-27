#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-v1.0.0}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

if [ -f "$HOME/.cargo/env" ]; then
  # shellcheck disable=SC1091
  source "$HOME/.cargo/env"
fi

echo "==> [1/4] Building Go binary ($VERSION)..."
cd "$PROJECT_DIR/go-api"
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$PROJECT_DIR/go-api/bin/go-api" ./cmd/server

echo "==> [2/4] Building Rust release binary ($VERSION)..."
cd "$PROJECT_DIR/rust-api"
cargo build --release

echo "==> [3/4] Installing immutable release directories & flipping atomic symlinks..."
GO_REL="/opt/polyglot-rust-mariadb/go-api/releases/$VERSION"
RUST_REL="/opt/polyglot-rust-mariadb/rust-api/releases/$VERSION"

sudo mkdir -p "$GO_REL" "$RUST_REL"
sudo install -m 0755 -o polyglot-go -g polyglot-go "$PROJECT_DIR/go-api/bin/go-api" "$GO_REL/go-api"
sudo install -m 0755 -o polyglot-rust -g polyglot-rust "$PROJECT_DIR/rust-api/target/release/polyglot-rust-api" "$RUST_REL/polyglot-rust-api"

sudo ln -sfn "$GO_REL" /opt/polyglot-rust-mariadb/go-api/current_tmp
sudo mv -Tf /opt/polyglot-rust-mariadb/go-api/current_tmp /opt/polyglot-rust-mariadb/go-api/current

sudo ln -sfn "$RUST_REL" /opt/polyglot-rust-mariadb/rust-api/current_tmp
sudo mv -Tf /opt/polyglot-rust-mariadb/rust-api/current_tmp /opt/polyglot-rust-mariadb/rust-api/current

echo "==> [4/4] Restarting systemd units & verifying health..."
sudo systemctl restart polyglot-rm-rust-api
sudo systemctl restart polyglot-rm-go-api
sleep 1

curl -fsS http://127.0.0.1/healthz && echo
curl -fsS http://127.0.0.1/api/v1/rust/readyz && echo
curl -fsS http://127.0.0.1/api/v1/go/readyz && echo
echo "Deployed release $VERSION successfully."
