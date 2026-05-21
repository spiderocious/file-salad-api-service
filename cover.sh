#!/usr/bin/env bash
# Coverage report. Runs unit + integration tests with cross-package coverage so
# the HTTP-level integration tests in internal/app count toward the feature
# packages they exercise (auth, uploads, webuploads, middleware, quota, ...).
#
# Integration tests need a reachable MongoDB (MONGODB_URI, default
# mongodb://localhost:27017). Redis is faked in-process (miniredis), so it isn't
# required. If Mongo is down the integration tests skip and coverage drops.
#
# Usage:
#   ./cover.sh            # text summary + total
#   ./cover.sh html       # also open an HTML report
set -euo pipefail

cd "$(dirname "$0")"

PROFILE=coverage.out

# -coverpkg makes coverage attribute across package boundaries (integration
# tests in app cover code in auth/uploads/etc). Build the integration tests too.
go test -tags=integration \
  -coverpkg=./internal/... \
  -coverprofile="$PROFILE" \
  ./... 2>&1 | grep -vE "no test files" || true

echo
echo "=== per-package coverage ==="
go tool cover -func="$PROFILE" | grep -vE "^total:" | awk '{print $1, $3}' \
  | sed -E 's#github.com/feranmi/file-salad-backend/##' | column -t || true

echo
TOTAL=$(go tool cover -func="$PROFILE" | grep '^total:' | awk '{print $3}')
echo "=== TOTAL COVERAGE: $TOTAL ==="

if [[ "${1:-}" == "html" ]]; then
  go tool cover -html="$PROFILE" -o coverage.html
  echo "wrote coverage.html"
fi
