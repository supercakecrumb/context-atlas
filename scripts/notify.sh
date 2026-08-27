#!/usr/bin/env bash
# Send a short progress message to Aurora's Telegram chat via the curly notifier.
#
# Usage: notify.sh "text of the message"
#
# Routes through curly (POST /send_notification) instead of the Telegram Bot API,
# so nothing that calls this — no repo, no CI job, no shell — ever holds a
# Telegram bot token. The only credential is a per-chat password:
# base64url(HMAC-SHA256(seed, telegram_id)), deterministic and non-reversible,
# so it authorizes sending to exactly one chat and nothing else.
#
# CREDENTIALS. Each of CURLY_URL / CURLY_TELEGRAM_ID / CURLY_PASSWORD resolves
# independently, first hit wins:
#
#   1. the process environment                     — CI and one-off overrides
#   2. the repo's .env, if this is running in one   — per-repo override
#   3. $CURLY_ENV_FILE, else the private workspace store — machine default
#
# Normally only (3) is populated: one file per machine, not one copy per repo.
#
# Files are PARSED, never sourced, and only CURLY_* keys are read — so a repo
# .env can't leak its DATABASE_URL into this process or execute code from it.
# The password is never echoed or logged. Sends are non-blocking by default;
# CURLY_STRICT=1 makes credential, network, and HTTP failures return nonzero.
#
# This exact file is installed in two places, and must keep working as both:
#   <repo>/scripts/notify.sh   (vendored, called by the post-commit hook)
#   ~/.local/bin/curly-notify  (on PATH, called by Claude Code hooks and rwtn)
set -uo pipefail

# ── where to look ────────────────────────────────────────────────────────────

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# The repo .env, but only when we're actually inside a repo. Vendored at
# <repo>/scripts/notify.sh the sibling is one level up; run from anywhere else
# (e.g. as ~/.local/bin/curly-notify) fall back to asking git where we are.
repo_env=""
if [ -n "${CURLY_REPO_ENV:-}" ]; then
  repo_env="$CURLY_REPO_ENV"
elif [ "$(basename "$script_dir")" = "scripts" ] && [ -f "$script_dir/../.env" ]; then
  repo_env="$(cd "$script_dir/.." && pwd)/.env"
else
  git_top="$(git rev-parse --show-toplevel 2>/dev/null)" || git_top=""
  [ -n "$git_top" ] && [ -f "$git_top/.env" ] && repo_env="$git_top/.env"
fi

private_root="${PERSONAL_WORKSPACE_PRIVATE_DIR:-$HOME/Library/Application Support/PersonalWorkspace}"
shared_env="${CURLY_ENV_FILE:-$private_root/secrets/workspace/curly.env}"

# ── credential resolution ────────────────────────────────────────────────────

# Read one CURLY_* key out of an env-style file WITHOUT executing the file.
# Last assignment wins, matching what sourcing would have done.
read_curly_key() {
  local file="$1" key="$2" line value
  [ -f "$file" ] || return 1
  line="$(grep -E "^[[:space:]]*(export[[:space:]]+)?${key}=" "$file" 2>/dev/null | tail -n 1)"
  [ -n "$line" ] || return 1

  value="${line#*=}"
  value="${value#"${value%%[![:space:]]*}"}"   # trim leading space

  case "$value" in
    \"*\"*) value="${value#\"}"; value="${value%%\"*}" ;;   # "quoted"
    \'*\'*) value="${value#\'}"; value="${value%%\'*}" ;;   # 'quoted'
    *)
      value="${value%%[[:space:]]#*}"                       # strip ` # comment`
      value="${value%"${value##*[![:space:]]}"}"            # trim trailing space
      ;;
  esac

  [ -n "$value" ] || return 1
  printf '%s' "$value"
}

resolve() {
  local key="$1" file found
  [ -n "${!key:-}" ] && return 0            # already in the environment — done
  for file in "$repo_env" "$shared_env"; do
    [ -n "$file" ] || continue
    found="$(read_curly_key "$file" "$key")" || continue
    printf -v "$key" '%s' "$found"
    return 0
  done
  return 1
}

resolve CURLY_URL
resolve CURLY_TELEGRAM_ID
resolve CURLY_PASSWORD

if [ -z "${CURLY_URL:-}" ] || [ -z "${CURLY_TELEGRAM_ID:-}" ] || [ -z "${CURLY_PASSWORD:-}" ]; then
  echo "notify: no curly credentials — set them in $shared_env (see the kit's templates/curly.env.example) — skipping" >&2
  [ "${CURLY_STRICT:-0}" = "1" ] && exit 2
  exit 0
fi

# ── send ─────────────────────────────────────────────────────────────────────

# curly relays with ParseMode=HTML, and it does it ASYNCHRONOUSLY: the endpoint
# returns 200 "Notification queued" as soon as the password checks out, then a
# goroutine calls SendMessage. So if Telegram rejects the text as malformed HTML
# the message is dropped and only curly's own log knows — this script, and the
# commit that triggered it, see a clean success. A subject with `<`, `>` or `&`
# in it (a Go repo: `&&`, `<-chan`, generics) would silently never arrive.
# So treat the argument as PLAIN TEXT and escape it. Set CURLY_RAW_HTML=1 to
# send real markup instead (then it's the caller's job to keep it valid).
html_escape() {
  local s="$1"
  s="${s//&/&amp;}"        # must run first, or it double-escapes the others
  s="${s//</&lt;}"
  s="${s//>/&gt;}"
  printf '%s' "$s"
}

# Build the JSON body safely (escape backslash, quote, control chars) so an
# arbitrary commit subject can't break the payload.
json_escape() {
  local s="$1"
  s="${s//\\/\\\\}"
  s="${s//\"/\\\"}"
  s="${s//$'\n'/\\n}"
  s="${s//$'\r'/\\r}"
  s="${s//$'\t'/\\t}"
  printf '%s' "$s"
}

text="${1:-(empty notify)}"
[ "${CURLY_RAW_HTML:-0}" = "1" ] || text="$(html_escape "$text")"
text="$(json_escape "$text")"

# We escape here and then declare format=html, meaning "this is already valid
# Telegram HTML, pass it through". Newer curly also escapes server-side when the
# format is text/absent, so declaring it is what stops the two layers from
# double-escaping (`&` → `&amp;` → `&amp;amp;`). Older curly ignores the field
# entirely, so one payload is correct against both — which matters while the old
# and new servers are both reachable. Once every caller is on the new server,
# this flips to sending plain text with no format at all.

# --data @- so the password never appears in the process table / ps output.
if ! printf '{"text":"%s","telegram_id":"%s","password":"%s","format":"html"}' \
  "$text" "$CURLY_TELEGRAM_ID" "$CURLY_PASSWORD" |
  curl -fsS -m 10 -X POST "$CURLY_URL" \
    -H "Content-Type: application/json" \
    --data @- \
    -o /dev/null; then
  echo "notify: send failed" >&2
  [ "${CURLY_STRICT:-0}" = "1" ] && exit 1
fi

exit 0
