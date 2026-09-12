#!/bin/sh
# GO-2026-5932 affects openpgp subpackages, not all of golang.org/x/crypto.
# Keep the container scan exception valid: none may enter a release binary.
set -eu

runtime_packages=$(go list -deps ./cmd/api ./cmd/grpc ./cmd/migrate)
if printf '%s\n' "$runtime_packages" | grep -Eq '^golang.org/x/crypto/openpgp(/|$)'; then
    echo 'Unsafe OpenPGP code is part of a release target; remove it and reevaluate GO-2026-5932.' >&2
    exit 1
fi
echo 'PASS: no golang.org/x/crypto/openpgp package in any release target'
