#!/usr/bin/env bash

set -euo pipefail

env_file=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/.env

if [[ -e $env_file ]]; then
    printf 'init-env: %s exists; nothing changed\n' "$env_file"
    exit 0
fi

command -v openssl > /dev/null || { printf 'init-env: openssl is not installed\n' >&2; exit 1; }

umask 077
cat > "$env_file" <<ENV
ZAPGATE_PUBLIC_URL=${ZAPGATE_PUBLIC_URL:-http://localhost:8080}
ZAPGATE_OWNER_PASSWORD=$(openssl rand -base64 30 | tr -d '/+=\n')
ZAPGATE_SIGNING_KEY=$(openssl rand -hex 32)
ZAPGATE_RETENTION_MONTHS=6
ZAPGATE_CLIENT_IP_HEADER=
CLOUDFLARE_TUNNEL_TOKEN=
ENV
printf 'init-env: wrote %s\n' "$env_file"
