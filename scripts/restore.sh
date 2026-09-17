#!/usr/bin/env sh
set -eu
: "${K12EDU_DATABASE_URL:?K12EDU_DATABASE_URL is required}"
: "${1:?usage: restore.sh backup.dump}"
pg_restore --clean --if-exists --no-owner --dbname="$K12EDU_DATABASE_URL" "$1"
