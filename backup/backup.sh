#!/bin/sh
# PosternDB backup sidecar: a small job runner.
#
# The app runs on a distroless image without pg_dump/pg_restore, so it writes
# jobs into the "jobs" table and this loop executes them:
#
#   dump         pg_dump a live database to /backups           (manual backup)
#   archive      pg_dump an archived pdbarch_* database, then drop it and its
#                same-named role                                (expiry/deletion)
#   restore      pg_restore a file into an (empty) database, owned by a role
#   clone        pg_dump | pg_restore from one database into another
#   delete_file  remove a dump from /backups
#
# Jobs are claimed with FOR UPDATE SKIP LOCKED, so even two sidecars would
# never run the same job twice. A failed dump never drops anything.
#
# Archived databases left without a job (e.g. created by an older version)
# are picked up automatically.
set -eu

INTERVAL="${BACKUP_POLL_SECONDS:-5}"
OUT_DIR=/backups
mkdir -p "$OUT_DIR"

q() { psql -v ON_ERROR_STOP=1 -X -q -At "$@"; }

# psql only interpolates :'variables' in SQL read from stdin (never in -c),
# so parameterised statements are piped in. :'var' quotes the value as a
# literal, which keeps file names and error text from breaking the SQL.
qv() {
  sql=$1
  shift
  printf '%s\n' "$sql" | q "$@"
}

finish() {
  # $1 = id, $2 = status, $3 = error text, $4 = bytes (optional)
  qv "UPDATE jobs SET status = :'status', error = :'err', bytes = NULLIF(:'bytes', '')::bigint, finished_at = now() WHERE id = :'id'::bigint" \
    -v id="$1" -v status="$2" -v err="$3" -v bytes="${4:-}" \
    || echo "warning: could not update job $1" >&2
}

audit() {
  qv "INSERT INTO audit_log (actor, action, target, details) VALUES ('backup-sidecar', :'action', :'target', :'details'::jsonb)" \
    -v action="$1" -v target="$2" -v details="$3" >/dev/null 2>&1 || true
}

safe_file() {
  # Only plain file names produced by the app are accepted.
  case "$1" in
    */* | .* | "") return 1 ;;
    *) return 0 ;;
  esac
}

dump_to() {
  # $1 = database, $2 = file name
  tmp="$OUT_DIR/$2.partial"
  if pg_dump --format=custom --dbname="$1" --file="$tmp" 2>"$OUT_DIR/.err"; then
    mv "$tmp" "$OUT_DIR/$2"
    return 0
  fi
  rm -f "$tmp"
  return 1
}

err_text() { head -c 500 "$OUT_DIR/.err" 2>/dev/null || true; }

run_job() {
  id=$1 kind=$2 slug=$3 src=$4 dst=$5 role=$6 file=$7
  case "$kind" in
    dump | archive)
      safe_file "$file" || { finish "$id" "failed" "invalid file name"; return; }
      if ! dump_to "$src" "$file"; then
        finish "$id" "failed" "$(err_text)"
        audit backup_failed "$slug" "{\"database\": \"$src\"}"
        return
      fi
      size=$(wc -c <"$OUT_DIR/$file" | tr -d ' ')
      if [ "$kind" = archive ]; then
        dropdb --if-exists --force "$src"
        qv 'DROP ROLE IF EXISTS :"role"' -v role="$src"
      fi
      finish "$id" "done" "" "$size"
      audit backup_completed "$slug" "{\"file\": \"$file\", \"bytes\": $size}"
      ;;
    restore)
      safe_file "$file" || { finish "$id" "failed" "invalid file name"; return; }
      if pg_restore --no-owner --no-privileges --role="$role" --dbname="$dst" "$OUT_DIR/$file" 2>"$OUT_DIR/.err"; then
        finish "$id" "done" ""
      else
        finish "$id" "failed" "$(err_text)"
      fi
      ;;
    clone)
      if pg_dump --format=custom --dbname="$src" 2>"$OUT_DIR/.err" |
        pg_restore --no-owner --no-privileges --role="$role" --dbname="$dst" 2>>"$OUT_DIR/.err"; then
        finish "$id" "done" ""
      else
        finish "$id" "failed" "$(err_text)"
      fi
      ;;
    delete_file)
      safe_file "$file" || { finish "$id" "failed" "invalid file name"; return; }
      rm -f "$OUT_DIR/$file"
      finish "$id" "done" ""
      ;;
    *)
      finish "$id" "failed" "unknown job kind"
      ;;
  esac
}

claim() {
  q -F '|' 2>/dev/null -c "UPDATE jobs SET status = 'running', started_at = now()
    WHERE id = (SELECT id FROM jobs WHERE status = 'pending' ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED)
    RETURNING id, kind, slug, source_db, target_db, target_role, file"
}

adopt_orphans() {
  q -c "INSERT INTO jobs (kind, slug, source_db, file, reason, requested_by)
    SELECT 'archive', '', d.datname, d.datname || '.dump', 'expiry', 'backup-sidecar'
    FROM pg_database d
    WHERE starts_with(d.datname, 'pdbarch_')
      AND NOT EXISTS (SELECT 1 FROM jobs j WHERE j.source_db = d.datname AND j.status IN ('pending', 'running', 'failed'))" \
    >/dev/null 2>&1 || true
}

echo "backup sidecar started (poll every ${INTERVAL}s)"
while true; do
  adopt_orphans
  while job=$(claim) && [ -n "$job" ]; do
    IFS='|' read -r id kind slug src dst role file <<EOF
$job
EOF
    echo "job $id: $kind $src$dst $file"
    run_job "$id" "$kind" "$slug" "$src" "$dst" "$role" "$file"
  done
  sleep "$INTERVAL"
done
