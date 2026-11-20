#!/usr/bin/env bash
# Rotates the master user ("admin") password of maintainer-d's OCI Database
# with PostgreSQL system. The password never appears in output, in argv, or
# in a pod spec.
#
#   scripts/db/rotate-admin-password.sh [--secret-file FILE] [--apply] [--dry-run]
#
#   --secret-file  SOPS-encrypted Secret whose stringData.MD_DB_DSN holds the
#                  admin DSN (default deploy/secrets/maintainerd-db-env.yaml,
#                  which the workloads load until they move to maintainerd_app).
#   --apply        apply that Secret to the cluster, remove any keys that exist
#                  only in the live Secret, and restart the Deployments that load it.
#   --dry-run      run every check and change nothing.
#
# Order: new password -> SOPS file -> OCI reset -> verify from a pod -> (apply,
# restart, verify as the workloads). Up to and including the OCI reset, any
# failure restores the SOPS file. After a successful reset, the new password
# is already in the SOPS file, so it can't be lost.
set -euo pipefail

NAMESPACE="${NAMESPACE:-maintainerd}"
DB_SYSTEM_NAME="${MD_DB_SYSTEM_NAME:-maintainerd-pg-db}"
SECRET_FILE="deploy/secrets/maintainerd-db-env.yaml"
DSN_PATH='["stringData"]["MD_DB_DSN"]'
PSQL_IMAGE="postgres:16-alpine"
APPLY=false
DRY_RUN=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --secret-file) SECRET_FILE="$2"; shift 2 ;;
    --apply) APPLY=true; shift ;;
    --dry-run) DRY_RUN=true; shift ;;
    -h|--help) sed -n '2,19p' "$0"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

log() { printf '%s %s\n' "$(date -u +%H:%M:%SZ)" "$*"; }
die() { log "ERROR: $*" >&2; exit 1; }

for tool in oci sops kubectl jq python3 git; do
  command -v "$tool" >/dev/null || die "missing required tool: $tool"
done

cd "$(git rev-parse --show-toplevel)"
[[ -f "$SECRET_FILE" ]] || die "missing $SECRET_FILE"
git diff --quiet -- "$SECRET_FILE" || die "$SECRET_FILE has uncommitted changes; commit or restore it first"

# Secret material lives only here: private, on tmpfs when XDG_RUNTIME_DIR is set, wiped on exit.
umask 077
WORK=$(mktemp -d "${XDG_RUNTIME_DIR:-/tmp}/md-rotate.XXXXXX")
RESET_SENT=false
FINISHED=false
cleanup() {
  find "$WORK" -type f -exec shred -u {} + 2>/dev/null || true
  rm -rf "$WORK"
  if $RESET_SENT && ! $FINISHED; then
    log "STOPPED after the OCI reset was sent. The new DSN is in $SECRET_FILE; do NOT git restore it." >&2
    log "Apply it with: sops -d $SECRET_FILE | kubectl -n $NAMESPACE apply -f -   then restart its consumers." >&2
  fi
}
trap cleanup EXIT

# --- Look up the DB system by name (no OCIDs or IPs in the repo) -------------
log "looking up OCI PostgreSQL DB system '$DB_SYSTEM_NAME'"
DB_SYSTEM_ID=$(oci search resource structured-search \
  --query-text "query PostgresqlDbSystem resources where displayName = '$DB_SYSTEM_NAME' && lifecycleState = 'ACTIVE'" \
  --query 'data.items[0].identifier' --raw-output)
[[ "$DB_SYSTEM_ID" == ocid1.postgresqldbsystem.* ]] || die "no ACTIVE DB system named '$DB_SYSTEM_NAME'"
oci psql db-system get --db-system-id "$DB_SYSTEM_ID" > "$WORK/dbsystem.json"
DB_HOST=$(jq -r '.data."network-details"."primary-db-endpoint-private-ip"' "$WORK/dbsystem.json")
ADMIN_USER=$(jq -r '.data."admin-username"' "$WORK/dbsystem.json")
log "DB system found: admin user '$ADMIN_USER', private endpoint resolved"

# --- Read and check the current DSN ------------------------------------------
CURRENT_DSN=$(sops -d --extract "$DSN_PATH" "$SECRET_FILE") || die "cannot decrypt $DSN_PATH from $SECRET_FILE"
SECRET_NAME=$(sops -d --extract '["metadata"]["name"]' "$SECRET_FILE")
[[ "$CURRENT_DSN" != *"'"* && "$CURRENT_DSN" != *\\* ]] || die "DSN uses quoted or escaped values; only plain key=value DSNs are supported"
read -r -a FIELDS <<<"$CURRENT_DSN"
pw_fields=0; dsn_user=""; dsn_host=""; dsn_db=""
for f in "${FIELDS[@]}"; do
  case "$f" in
    password=*) pw_fields=$((pw_fields + 1)) ;;
    user=*) dsn_user="${f#user=}" ;;
    host=*) dsn_host="${f#host=}" ;;
    dbname=*) dsn_db="${f#dbname=}" ;;
  esac
done
[[ $pw_fields -eq 1 ]] || die "expected exactly one password= field in the DSN, found $pw_fields"
[[ "$dsn_user" == "$ADMIN_USER" ]] || die "$SECRET_FILE connects as '$dsn_user', not the master user '$ADMIN_USER'; use --secret-file for the admin DSN"
[[ "$dsn_host" == "$DB_HOST" ]] || die "DSN host doesn't match the DB system's private endpoint"
[[ -n "$dsn_db" ]] || die "DSN has no dbname"
log "current DSN in $SECRET_FILE (Secret '$SECRET_NAME'): user=$dsn_user, ${#FIELDS[@]} fields, host matches"

# --- Generate the new password -----------------------------------------------
# 32 chars: OCI requires upper, lower, digit and special. Specials are limited
# to characters that need no quoting in a libpq key=value DSN.
NEW_PASSWORD=$(python3 -I -c '
import secrets, string
rng = secrets.SystemRandom()
alnum = string.ascii_letters + string.digits
while True:
    pw = [rng.choice(alnum) for _ in range(27)] + [rng.choice("-_.+") for _ in range(2)]
    pw += [rng.choice(string.ascii_uppercase), rng.choice(string.ascii_lowercase), rng.choice(string.digits)]
    rng.shuffle(pw)
    if pw[0].isalnum():
        print("".join(pw)); break
')
NEW_FIELDS=()
for f in "${FIELDS[@]}"; do
  if [[ "$f" == password=* ]]; then NEW_FIELDS+=("password=$NEW_PASSWORD"); else NEW_FIELDS+=("$f"); fi
done
NEW_DSN="${NEW_FIELDS[*]}"
log "new password generated (length ${#NEW_PASSWORD})"

# --- Preconditions in the cluster --------------------------------------------
log "kube context: $(kubectl config current-context), namespace: $NAMESPACE"
# shellcheck disable=SC2016 # $s is a jq variable
uses_secret='[.spec.template.spec.containers[] | (.envFrom[]?.secretRef.name), (.env[]?.valueFrom.secretKeyRef.name)] | index($s)'
mapfile -t DEPLOYS < <(kubectl -n "$NAMESPACE" get deploy -o json | jq -r --arg s "$SECRET_NAME" ".items[] | select($uses_secret) | .metadata.name")
mapfile -t ACTIVE_JOBS < <(kubectl -n "$NAMESPACE" get jobs -o json | jq -r --arg s "$SECRET_NAME" ".items[] | select((.status.active // 0) > 0) | select($uses_secret) | .metadata.name")
log "Deployments loading '$SECRET_NAME': ${DEPLOYS[*]:-none}"
if [[ ${#ACTIVE_JOBS[@]} -gt 0 ]]; then
  log "WARNING: running Jobs load '$SECRET_NAME' and will fail to reconnect: ${ACTIVE_JOBS[*]}"
fi

if $DRY_RUN; then
  log "dry run: all checks passed, nothing changed"
  exit 0
fi

# --- 1. Write the new DSN into the SOPS file ---------------------------------
cp -p "$SECRET_FILE" "$WORK/secret-file.enc"
restore_secret_file() { cp -p "$WORK/secret-file.enc" "$SECRET_FILE"; log "restored $SECRET_FILE"; }
printf '%s' "$NEW_DSN" | jq -Rsj @json | sops set --value-stdin "$SECRET_FILE" "$DSN_PATH" \
  || { restore_secret_file; die "sops set failed"; }
[[ "$(sops -d --extract "$DSN_PATH" "$SECRET_FILE")" == "$NEW_DSN" ]] \
  || { restore_secret_file; die "SOPS file did not round-trip the new DSN"; }
log "1/4 SOPS file updated"

# --- 2. Reset the master password in OCI -------------------------------------
# The password reaches the CLI through a 0600 file, not argv.
# The reset is synchronous: it creates no work request, so --wait-for-state
# must not be used (the CLI then prints an error to stdout and the JSON after it).
# Whether the password changed is decided by connecting with it (step 3), not by
# parsing CLI output.
PW="$NEW_PASSWORD" jq -n --arg id "$DB_SYSTEM_ID" '{dbSystemId: $id, passwordDetailsPassword: env.PW}' > "$WORK/reset.json"
log "2/4 resetting the master password in OCI"
reset_rc=0
oci psql db-system reset-master-user-password-plain-text-password-details \
  --from-json "file://$WORK/reset.json" > "$WORK/reset.out" 2> "$WORK/reset.err" || reset_rc=$?
log "2/4 OCI CLI exited $reset_rc"

# From here on the password in OCI may have changed. The new DSN is already in
# the SOPS file, and it must never be restored to the old one unless we've
# proved the old password still works.
RESET_SENT=true

# --- 3. Verify the new password from inside the cluster ----------------------
# The password reaches psql on stdin; the pod spec holds nothing secret.
try_password() { # $1 = password; prints the connected user on success
  printf '%s\n' "$1" | kubectl -n "$NAMESPACE" run "md-rotate-verify-$$-$RANDOM" --rm -i --restart=Never --quiet \
    --image="$PSQL_IMAGE" --command -- sh -c \
    "read -r PGPASSWORD; export PGPASSWORD; exec psql -X -w -tA 'host=$DB_HOST port=5432 user=$ADMIN_USER dbname=$dsn_db sslmode=require connect_timeout=10' -c 'select current_user' 2>/dev/null"
}
log "3/4 verifying the new password from a temporary pod (up to 5 minutes)"
who=""
for attempt in $(seq 1 10); do
  if who=$(try_password "$NEW_PASSWORD") && [[ "$who" == "$ADMIN_USER" ]]; then break; fi
  who=""
  log "3/4 attempt $attempt: new password not accepted yet"
  sleep 30
done
if [[ -z "$who" ]]; then
  OLD_PASSWORD=""
  for f in "${FIELDS[@]}"; do [[ "$f" == password=* ]] && OLD_PASSWORD="${f#password=}"; done
  if [[ $reset_rc -ne 0 ]] && [[ "$(try_password "$OLD_PASSWORD" || true)" == "$ADMIN_USER" ]]; then
    RESET_SENT=false
    restore_secret_file
    die "OCI reset failed (exit $reset_rc) and the old password still works; nothing changed: $(grep -m1 -E '"message"|Error' "$WORK/reset.err" || echo 'see oci CLI error')"
  fi
  die "the new password is not accepted after 5 minutes, and the old one isn't confirmed working. Investigate in the OCI console."
fi
log "3/4 new password works (connected as $who)"

if ! $APPLY; then
  FINISHED=true
  log "done. Not applied to the cluster (no --apply). Commit $SECRET_FILE."
  exit 0
fi

# --- 4. Apply, prune stale keys, restart, verify as the workloads ------------
log "4/4 applying Secret '$SECRET_NAME'"
sops -d "$SECRET_FILE" | kubectl -n "$NAMESPACE" apply -f - >/dev/null
mapfile -t STALE < <(comm -13 \
  <(sops -d --output-type json "$SECRET_FILE" | jq -r '((.stringData // {}) + (.data // {})) | keys[]' | sort) \
  <(kubectl -n "$NAMESPACE" get secret "$SECRET_NAME" -o json | jq -r '.data | keys[]' | sort))
for key in "${STALE[@]}"; do
  kubectl -n "$NAMESPACE" patch secret "$SECRET_NAME" --type=json -p "[{\"op\":\"remove\",\"path\":\"/data/$key\"}]" >/dev/null
  log "removed key '$key', which existed only in the live Secret"
done
for d in "${DEPLOYS[@]}"; do
  kubectl -n "$NAMESPACE" rollout restart "deploy/$d" >/dev/null
done
for d in "${DEPLOYS[@]}"; do
  kubectl -n "$NAMESPACE" rollout status "deploy/$d" --timeout=300s >/dev/null || die "deploy/$d did not become ready"
  log "deploy/$d restarted and ready"
done
verify_pod="md-rotate-verify-secret-$$"
who=$(kubectl -n "$NAMESPACE" run "$verify_pod" --rm -i --restart=Never --quiet --image="$PSQL_IMAGE" \
  --overrides="{\"spec\":{\"containers\":[{\"name\":\"$verify_pod\",\"image\":\"$PSQL_IMAGE\",\"command\":[\"sh\",\"-c\",\"exec psql -X -w -tA \\\"\$MD_DB_DSN\\\" -c 'select current_user'\"],\"envFrom\":[{\"secretRef\":{\"name\":\"$SECRET_NAME\"}}]}]}}") \
  || die "a pod loading Secret '$SECRET_NAME' could not connect"
log "4/4 Secret '$SECRET_NAME' connects (as $who)"
FINISHED=true
log "done. Commit $SECRET_FILE (encrypted) in a PR; CronJobs pick up the Secret on their next run."
