#!/bin/sh
# Runs Leafen's published image (or --image's) as one container with a finite
# memory limit, the "leafen-data" volume and the "leafen-backups" volume its
# backups go to (/backups; docs/self-hosting.md, "Docker" and "Backup").
set -eu

image=ghcr.io/leafen-org/leafen:latest
name=leafen
port=8090
volume=leafen-data
backups=leafen-backups
print_only=false

while [ "$#" -gt 0 ]; do
  case "$1" in
    --image)
      image=$2
      shift 2
      ;;
    --name)
      name=$2
      shift 2
      ;;
    --port)
      port=$2
      shift 2
      ;;
    --volume)
      volume=$2
      shift 2
      ;;
    --backups)
      backups=$2
      shift 2
      ;;
    --print)
      print_only=true
      shift
      ;;
    --help|-h)
      printf '%s\n' 'Usage: scripts/run-self-hosted.sh [--image IMAGE] [--name NAME] [--port PORT] [--volume VOLUME] [--backups VOLUME] [--print]'
      exit 0
      ;;
    *)
      printf '%s\n' "Unknown option: $1" >&2
      exit 2
      ;;
  esac
done

if [ "$print_only" = true ] && [ -n "${LEAFEN_MEMORY_LIMIT:-}" ]; then
  memory_limit=$LEAFEN_MEMORY_LIMIT
else
  if [ -n "${LEAFEN_MEMORY_LIMIT:-}" ]; then
    memory_limit=$(docker run --rm \
      -e "LEAFEN_MEMORY_LIMIT=$LEAFEN_MEMORY_LIMIT" \
      --entrypoint /usr/local/bin/leafen \
      "$image" memory-limit)
  else
    memory_limit=$(docker run --rm \
      --entrypoint /usr/local/bin/leafen \
      "$image" memory-limit)
  fi
fi

if [ -z "$memory_limit" ]; then
  printf '%s\n' 'Leafen memory policy returned an empty limit.' >&2
  exit 1
fi

# Pass the variable's name, so Docker inherits it without the access key
# appearing in process arguments or the printed command.
set --
cover_key_option=
if [ -n "${LEAFEN_UNSPLASH_ACCESS_KEY:-}" ]; then
  set -- --env LEAFEN_UNSPLASH_ACCESS_KEY
  cover_key_option=' --env LEAFEN_UNSPLASH_ACCESS_KEY'
fi

for variable in LEAFEN_PLACE_SEARCH_PROVIDER LEAFEN_PLACE_SEARCH_URL LEAFEN_PLACE_SEARCH_API_KEY LEAFEN_PLACE_SEARCH_API_KEY_ID LEAFEN_PLACE_SEARCH_DISABLED; do
  # Only these fixed environment names are expanded; their values never enter
  # command arguments or --print output.
  if printenv "$variable" >/dev/null 2>&1; then
    set -- "$@" --env "$variable"
    cover_key_option="$cover_key_option --env $variable"
  fi
done

printf '%s\n' "Leafen memory policy: $memory_limit (hard and swap limits are equal)." >&2
if [ "$print_only" = true ]; then
  printf 'docker run --name %s --restart unless-stopped --memory %s --memory-swap %s -p %s:8090 --env LEAFEN_PUBLISHED_PORT=%s -v %s:/data -v %s:/backups%s %s\n' \
    "$name" "$memory_limit" "$memory_limit" "$port" "$port" "$volume" "$backups" "$cover_key_option" "$image"
  exit 0
fi

exec docker run \
  --name "$name" \
  --restart unless-stopped \
  --memory "$memory_limit" \
  --memory-swap "$memory_limit" \
  -p "$port:8090" \
  --env "LEAFEN_PUBLISHED_PORT=$port" \
  -v "$volume:/data" \
  -v "$backups:/backups" \
  "$@" \
  "$image"
