#!/bin/bash
# Applies every migration and seed file in lexical order during first-time
# database initialisation. The postgres entrypoint ignores mounted
# directories, so this loader runs them explicitly.
set -euo pipefail

apply_dir() {
    local dir="$1"
    [ -d "$dir" ] || return 0
    for file in "$dir"/*.sql; do
        [ -e "$file" ] || continue
        echo "sentinel-grid: applying $file"
        psql --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
            --set ON_ERROR_STOP=1 --no-psqlrc --file "$file"
    done
}

apply_dir /sentinel-sql/migrations
apply_dir /sentinel-sql/seed
