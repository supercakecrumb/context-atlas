#!/usr/bin/env bash
# Single verification gate for a Go repo's commits.
#
# Runs every check that would otherwise be run by hand before a commit: formatting,
# build, vet, unit tests, (optionally) integration tests against a local Postgres,
# lint, changie fragment validation, and a staged-secrets scan. Fails fast on the
# first problem. The loop is always: edit -> ./scripts/pre-commit.sh -> commit -> push.
#
# Usage: ./scripts/pre-commit.sh   (run from the repo root)
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

# ── PER-REPO CONFIG (override via env if the defaults don't fit) ───────────────
# Defaults derive from the repo directory name, matching the convention
# <repo>-db-1 / <repo>_test / <REPO>_TEST_DATABASE_URL used across these repos.
repo_name="$(basename "$repo_root")"
DB_CONTAINER="${PRECOMMIT_DB_CONTAINER:-${repo_name}-db-1}"   # local Docker Postgres
TEST_DB="${PRECOMMIT_TEST_DB:-${repo_name}_test}"             # name MUST contain "test"
# Env var the integration tests read for their DSN. Keep whatever your test code
# expects — e.g. GEODRILL_TEST_DATABASE_URL, or the generic TEST_DATABASE_URL.
TEST_DSN_VAR="${PRECOMMIT_TEST_DSN_VAR:-TEST_DATABASE_URL}"

# Self-install the commit-notification hook (idempotent).
git config core.hooksPath githooks

start_time=$(date +%s)

section() {
  echo ""
  echo "=== $1 ==="
}

section "gofmt"
fmt_out="$(gofmt -l .)"
if [ -n "$fmt_out" ]; then
  echo "gofmt found unformatted files:"
  echo "$fmt_out"
  exit 1
fi
echo "OK — no unformatted files"

section "go build ./..."
go build ./...

section "go vet ./..."
go vet ./...

section "go test ./..."
go test ./...

if [ ! -x web/node_modules/.bin/orval ]; then
  echo "Frontend dependencies are missing; run 'npm --prefix web ci' before this gate."
  exit 1
fi

section "generated API contract"
make contracts-check

section "frontend"
npm --prefix web run typecheck
npm --prefix web test -- --run
npm --prefix web run build

section "integration tests"
if [ -f .env ]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a

  if command -v docker >/dev/null 2>&1 && docker exec "$DB_CONTAINER" true >/dev/null 2>&1; then
    docker exec "$DB_CONTAINER" psql -U "${POSTGRES_USER:-postgres}" -c "CREATE DATABASE ${TEST_DB}" >/dev/null 2>&1 || true
    export "${TEST_DSN_VAR}=postgres://${POSTGRES_USER:-postgres}:${POSTGRES_PASSWORD:-postgres}@localhost:${POSTGRES_PORT:-5432}/${TEST_DB}?sslmode=disable"
    go test -p 1 ./...
  else
    echo "NOTICE: docker or ${DB_CONTAINER} unavailable — skipping integration tests"
  fi
else
  echo "NOTICE: .env not found — skipping integration tests"
fi

section "golangci-lint"
golangci-lint run ./...

section "changie batch --dry-run patch"
if [ ! -d .changes/unreleased ]; then
  echo "NOTICE: no unreleased fragments — nothing to batch"
else
  changie_out="$(mktemp)"
  trap 'rm -f "$changie_out"' EXIT
  if ! changie batch --dry-run patch >"$changie_out" 2>&1; then
    if grep -qi "no unreleased changes" "$changie_out"; then
      echo "NOTICE: no unreleased fragments — nothing to batch"
    else
      cat "$changie_out"
      exit 1
    fi
  else
    cat "$changie_out"
  fi
fi

section "staged secrets scan"
staged_files="$(git diff --cached --name-only || true)"
bad=""
if [ -n "$staged_files" ]; then
  while IFS= read -r f; do
    [ -z "$f" ] && continue
    case "$f" in
      *.env.example)
        continue
        ;;
    esac
    case "$f" in
      *.env | *.log | *.dump | data/* | */data/*)
        bad="${bad}${f}"$'\n'
        ;;
    esac
  done <<<"$staged_files"
fi
if [ -n "$bad" ]; then
  echo "BLOCKED: staged files match secret/data patterns:"
  printf '%s' "$bad"
  exit 1
fi
echo "OK — no secrets staged"

end_time=$(date +%s)
echo ""
echo "=== pre-commit checks passed in $((end_time - start_time))s ==="
