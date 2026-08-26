#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$SCRIPT_DIR"
REPO="th3ee9ine/qqq2api"
PROXY="http://127.0.0.1:7890"
HOST_PROXY="http://host.docker.internal:7890"
VERSION=$(tr -d '[:space:]' < "$PROJECT_DIR/VERSION")

if [ -z "$VERSION" ]; then
  echo "ERROR: VERSION file is empty"
  exit 1
fi

echo "==> Building and pushing ${REPO}:${VERSION} and ${REPO}:latest (linux/amd64 + linux/arm64)"

HTTPS_PROXY="$PROXY" HTTP_PROXY="$PROXY" \
docker buildx build \
  --platform "linux/amd64,linux/arm64" \
  --build-arg "VERSION=$VERSION" \
  --build-arg "http_proxy=$HOST_PROXY" \
  --build-arg "https_proxy=$HOST_PROXY" \
  -t "${REPO}:${VERSION}" \
  -t "${REPO}:latest" \
  --push \
  "$PROJECT_DIR"

echo "==> Done: ${REPO}:${VERSION} and ${REPO}:latest (amd64 + arm64)"
