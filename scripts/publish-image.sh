#!/bin/sh
# Builds Leafen's release image for amd64 and arm64 and pushes it to a
# registry as <image>:<version> and <image>:latest (docs/development.md,
# "Release builds"). The release is built with the leafen_release tag and the
# version, which trusts only the production signing keys: without them in
# internal/app/release_keys_production.go the image refuses to start. Pushing
# needs `docker login` to the registry first; nothing here holds a secret.
#
#   scripts/publish-image.sh [--version 0.1.0] [--image ghcr.io/leafen-org/leafen] [--no-latest] [--development] [--print]
#
# The version defaults to web/package.json's, without a pre-release.
# --development builds without the release tag, as a development build whose
# version names "dev" (0.2.0-dev unless given): to try the procedure on a
# local registry. --print shows the build command without running it.
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
image=ghcr.io/leafen-org/leafen
version=
latest=true
development=false
print_only=false
source_url=https://github.com/leafen-org/leafen

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      version=$2
      shift 2
      ;;
    --image)
      image=$2
      shift 2
      ;;
    --no-latest)
      latest=false
      shift
      ;;
    --development)
      development=true
      shift
      ;;
    --print)
      print_only=true
      shift
      ;;
    --help|-h)
      printf '%s\n' 'Usage: scripts/publish-image.sh [--version VERSION] [--image IMAGE] [--no-latest] [--development] [--print]'
      exit 0
      ;;
    *)
      printf '%s\n' "Unknown option: $1" >&2
      exit 2
      ;;
  esac
done

if [ -z "$version" ]; then
  version=$(sed -n 's/^  "version": "\([0-9]*\.[0-9]*\.[0-9]*\).*",$/\1/p' "$root/web/package.json")
  [ -n "$version" ] || { printf '%s\n' 'web/package.json names no version; give --version.' >&2; exit 2; }
  if [ "$development" = true ]; then
    version="$version-dev"
  fi
fi

if [ "$development" = true ]; then
  build_tags=
  case "$version" in
    *-*dev*) ;;
    *)
      printf '%s\n' "A development build's version names \"dev\" (0.1.0-dev.1), not $version: it would refuse to start." >&2
      exit 2
      ;;
  esac
else
  build_tags=leafen_release
fi

set -- --platform linux/amd64,linux/arm64 \
  --build-arg "LEAFEN_BUILD_TAGS=$build_tags" \
  --build-arg "LEAFEN_VERSION=$version" \
  --label "org.opencontainers.image.version=$version" \
  --label "org.opencontainers.image.source=$source_url" \
  --annotation "index:org.opencontainers.image.source=$source_url" \
  --annotation "index:org.opencontainers.image.licenses=AGPL-3.0-only" \
  --tag "$image:$version"
if [ "$latest" = true ]; then
  set -- "$@" --tag "$image:latest"
fi
set -- "$@" --push "$root"

if [ "$print_only" = true ]; then
  printf 'docker buildx build'
  printf ' %s' "$@"
  printf '\n'
  exit 0
fi

printf '%s\n' "Building $image:$version${build_tags:+ ($build_tags)} for linux/amd64 and linux/arm64 and pushing it." >&2
docker buildx build "$@"
docker buildx imagetools inspect "$image:$version"
