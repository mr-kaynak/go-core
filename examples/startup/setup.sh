#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
if [[ -e .env ]]; then
  echo '.env already exists; keeping it.'
  exit 0
fi
umask 077
# noclobber also protects against another setup process creating .env.
set -o noclobber
{
  cat .env.example
  printf '\n'
  for key in JWT_SECRET JWT_REFRESH_SECRET SECURITY_ENCRYPTION_KEY; do
    printf '%s=%s\n' "$key" "$(openssl rand -hex 32)"
  done
} > .env
echo 'Created .env with fresh local secrets.'
