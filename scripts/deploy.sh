#!/usr/bin/env bash
# Deploy a published release. Keep the API token in the operator's environment.
set -euo pipefail
version="${1:-}"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'Usage: scripts/deploy.sh X.Y.Z'; exit 2; }
: "${DOKPLOY_URL:?required}" "${DOKPLOY_API_TOKEN:?required}" "${DOKPLOY_APP_ID:?required}" "${HEALTH_URL:?required}"
api() { curl --fail --silent --show-error --max-time 30 -H "x-api-key: $DOKPLOY_API_TOKEN" -H 'Content-Type: application/json' "$@"; }
image="ghcr.io/supercakecrumb/context-atlas:$version"
app=$(api "$DOKPLOY_URL/api/application.one?applicationId=$DOKPLOY_APP_ID")
for key in ${REQUIRE_ENV:-DATABASE_URL}; do
  jq -e --arg key "$key" '(.env // "") | split("\n") | any(startswith($key + "=") and (split("=")[1:] | join("=") | length > 0))' <<< "$app" >/dev/null
done
jq -nc --arg id "$DOKPLOY_APP_ID" --arg image "$image" '{applicationId:$id,dockerImage:$image,username:null,password:null,registryUrl:null}' \
  | api -X POST --data-binary @- "$DOKPLOY_URL/api/application.saveDockerProvider" >/dev/null
before=$(api "$DOKPLOY_URL/api/deployment.all?applicationId=$DOKPLOY_APP_ID" | jq -r 'sort_by(.createdAt)|last|.deploymentId // ""')
jq -nc --arg id "$DOKPLOY_APP_ID" '{applicationId:$id}' \
  | api -X POST --data-binary @- "$DOKPLOY_URL/api/application.deploy" >/dev/null
deadline=$((SECONDS+900))
while :; do
  read -r id status < <(api "$DOKPLOY_URL/api/deployment.all?applicationId=$DOKPLOY_APP_ID" \
    | jq -r 'sort_by(.createdAt)|last|"\(.deploymentId // "") \(.status // "")"')
  if [ -n "$id" ] && [ "$id" != "$before" ]; then
    echo "Deployment $id: $status"
    [ "$status" != error ] || exit 1
    [ "$status" != cancelled ] || exit 1
    [ "$status" != 'done' ] || break
  fi
  [ "$SECONDS" -lt "$deadline" ] || { echo 'Deployment timed out'; exit 1; }
  sleep 10
done
api "$DOKPLOY_URL/api/application.one?applicationId=$DOKPLOY_APP_ID" \
  | jq -e --arg image "$image" '.dockerImage == $image' >/dev/null
for _ in $(seq 1 30); do
  if curl --fail --silent --show-error --max-time 10 "$HEALTH_URL" >/dev/null; then
    echo "Healthy: $image at $HEALTH_URL"
    exit 0
  fi
  sleep 10
done
echo 'Public health check failed'
exit 1
