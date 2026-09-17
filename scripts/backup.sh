#!/usr/bin/env sh
set -eu
: "${K12EDU_DATABASE_URL:?K12EDU_DATABASE_URL is required}"
out="${1:-k12edu-$(date +%Y%m%d-%H%M%S).dump}"
pg_dump --format=custom --no-owner --file="$out" "$K12EDU_DATABASE_URL"
echo "database backup written to $out"
