#!/usr/bin/env bash

set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
HARNESS_ROOT=${HARNESS_ROOT:-"$ROOT/tmp/harness"}
HARNESS_ID=${HARNESS_ID:-default}
HARNESS_MODE=${HARNESS_MODE:-local}
INSTANCE="$HARNESS_ROOT/$HARNESS_ID"
MANIFEST="$INSTANCE/manifest.json"
MARKER="$INSTANCE/.media-gate-harness"

usage() {
  cat <<'EOF'
Usage: harness/harness.sh <command> [service]

Commands:
  up                 Start and seed an isolated instance
  status             Show process and endpoint status
  logs [service]     Show logs for all, backend, frontend, or fakes
  smoke              Run the deterministic API/download/import smoke test
  complete           Complete every torrent in the fake qBittorrent
  error              Put every fake torrent into an error state
  reset-fakes        Clear fake tracker/qBittorrent state and payloads
  down               Stop processes but preserve data and logs
  destroy            Stop processes and delete the instance directory
  ci                  Build, start, smoke-test, and destroy a CI instance

Environment:
  HARNESS_ID          Instance name (default: default)
  HARNESS_MODE        local (Air + Vite) or ci (built single binary)
  HARNESS_API_PORT    Optional fixed backend port
  HARNESS_UI_PORT     Optional fixed Vite port
  HARNESS_FAKE_PORT   Optional fixed fake integration port
  HARNESS_TMDB_APIKEY Optional explicit live metadata key
  HARNESS_TVDB_APIKEY Optional explicit live metadata key
  FOLLOW=1            Follow logs instead of printing the last 200 lines
EOF
}

fail() {
  printf 'harness: %s\n' "$*" >&2
  return 1
}

validate_id() {
  [[ "$HARNESS_ID" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || fail "invalid HARNESS_ID: $HARNESS_ID"
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

acquire_instance_lock() {
  [[ "${INSTANCE_LOCKED:-0}" == "1" ]] && return 0
  mkdir -p "$HARNESS_ROOT/.locks" || return 1
  exec 9>"$HARNESS_ROOT/.locks/$HARNESS_ID.lock" || return 1
  if ! flock -n 9; then
    fail "another lifecycle command is running for instance $HARNESS_ID"
    return 1
  fi
  INSTANCE_LOCKED=1
}

acquire_startup_lock() {
  exec 8>"/tmp/media-gate-harness-$UID.startup.lock"
  flock 8
}

release_startup_lock() {
  flock -u 8
  exec 8>&-
}

validate_requested_port() {
  local port=$1
  local label=$2
  [[ -z "$port" ]] && return 0
  [[ "$port" =~ ^[0-9]+$ ]] && ((port > 0 && port < 65536)) \
    || fail "invalid $label port: $port"
}

allocate_ports() {
  validate_requested_port "${HARNESS_API_PORT:-}" backend
  validate_requested_port "${HARNESS_UI_PORT:-}" frontend
  validate_requested_port "${HARNESS_FAKE_PORT:-}" fake

  local requested=("${HARNESS_API_PORT:-0}" "${HARNESS_FAKE_PORT:-0}")
  if [[ "$HARNESS_MODE" == "local" ]]; then
    requested+=("${HARNESS_UI_PORT:-0}")
  fi

  local allocated
  allocated=$(node - "${requested[@]}" <<'NODE'
import net from 'node:net'

const requested = process.argv.slice(2).map(Number)
const servers = []

try {
  for (const port of requested) {
    const server = net.createServer()
    await new Promise((resolve, reject) => {
      server.once('error', reject)
      server.listen({ host: '127.0.0.1', port }, resolve)
    })
    servers.push(server)
  }
  process.stdout.write(servers.map((server) => server.address().port).join(' '))
} finally {
  await Promise.all(servers.map((server) => new Promise((resolve) => server.close(resolve))))
}
NODE
  )

  if [[ "$HARNESS_MODE" == "local" ]]; then
    read -r API_PORT FAKE_PORT UI_PORT <<<"$allocated"
  else
    read -r API_PORT FAKE_PORT <<<"$allocated"
    UI_PORT=""
  fi
}

wait_http() {
  local url=$1
  local label=$2
  local attempts=${3:-120}
  local pid_file=${4:-}
  local i
  for ((i = 0; i < attempts; i++)); do
    if curl --silent --show-error --fail --max-time 1 "$url" >/dev/null 2>&1; then
      return
    fi
    if [[ -n "$pid_file" ]] && ! pid_running "$pid_file"; then
      printf 'harness: %s exited before becoming ready at %s\n' "$label" "$url" >&2
      return 1
    fi
    sleep 0.25 || true
  done
  printf 'harness: %s did not become ready at %s\n' "$label" "$url" >&2
  return 1
}

pid_running() {
  local file=$1
  [[ -f "$file" ]] || return 1
  local pid
  pid=$(<"$file")
  [[ "$pid" =~ ^[0-9]+$ ]] || return 1
  load_pid_identity "$file" "$pid" || return 1
  if kill -0 "$pid" 2>/dev/null; then
    pid_identity_matches "$pid" || return 1
  fi
  session_has_live_processes "$IDENTITY_SESSION"
}

read_process_identity() {
  local pid=$1
  [[ -r "/proc/$pid/stat" ]] || return 1
  local stat fields
  stat=$(<"/proc/$pid/stat")
  stat=${stat##*) }
  read -r -a fields <<<"$stat"
  ((${#fields[@]} >= 20)) || return 1
  PROCESS_STATE=${fields[0]}
  PROCESS_GROUP=${fields[2]}
  PROCESS_SESSION=${fields[3]}
  PROCESS_START=${fields[19]}
}

session_has_live_processes() {
  local session=$1
  local stat_file stat fields
  for stat_file in /proc/[0-9]*/stat; do
    [[ -r "$stat_file" ]] || continue
    stat=$(<"$stat_file") 2>/dev/null || continue
    stat=${stat##*) }
    read -r -a fields <<<"$stat"
    ((${#fields[@]} >= 4)) || continue
    if [[ "${fields[3]}" == "$session" && "${fields[0]}" != "Z" && "${fields[0]}" != "X" ]]; then
      return 0
    fi
  done
  return 1
}

signal_session() {
  local session=$1
  local signal=$2
  setsid "$INSTANCE/bin/harness-signal" \
    --leader "$session" --start-time "$IDENTITY_START" --signal "$signal"
}

session_has_live_subgroups() {
  local session=$1
  local root_group=$2
  local stat_file stat fields
  for stat_file in /proc/[0-9]*/stat; do
    [[ -r "$stat_file" ]] || continue
    stat=$(<"$stat_file") 2>/dev/null || continue
    stat=${stat##*) }
    read -r -a fields <<<"$stat"
    ((${#fields[@]} >= 4)) || continue
    if [[ "${fields[3]}" == "$session" && "${fields[2]}" != "$root_group" \
      && "${fields[0]}" != "Z" && "${fields[0]}" != "X" ]]; then
      return 0
    fi
  done
  return 1
}

signal_session_subgroups() {
  local session=$1
  local root_group=$2
  local signal=$3
  setsid "$INSTANCE/bin/harness-signal" \
    --leader "$session" --start-time "$IDENTITY_START" --signal "$signal" \
    --exclude-group "$root_group"
}

record_pid() {
  local service=$1
  local pid=$2
  local file="$INSTANCE/pids/$service.pid"
  local started="" group="" session=""
  local i
  for ((i = 0; i < 20; i++)); do
    read_process_identity "$pid" || break
    [[ -z "$started" ]] && started=$PROCESS_START
    [[ "$PROCESS_START" == "$started" ]] || break
    group=$PROCESS_GROUP
    session=$PROCESS_SESSION
    [[ "$group" == "$pid" && "$session" == "$pid" ]] && break
    sleep 0.05 || true
  done
  if [[ "$group" != "$pid" || "$session" != "$pid" ]]; then
    setsid "$INSTANCE/bin/harness-signal" \
      --leader "$pid" --start-time "$started" --signal TERM --target-only 2>/dev/null || true
    fail "process $pid was not started in an isolated session"
    return 1
  fi

  local identity_tmp="$file.identity.$$"
  local pid_tmp="$file.$$"
  if ! printf '%s %s %s %s\n' "$pid" "$started" "$group" "$session" >"$identity_tmp" \
    || ! mv -f "$identity_tmp" "$file.identity" \
    || ! printf '%s\n' "$pid" >"$pid_tmp" \
    || ! mv -f "$pid_tmp" "$file"; then
    rm -f "$identity_tmp" "$pid_tmp" "$file.identity" "$file"
    setsid "$INSTANCE/bin/harness-signal" \
      --leader "$pid" --start-time "$started" --signal TERM 2>/dev/null || true
    return 1
  fi
}

finish_service_start() {
  local service=$1
  local pid=$2
  if ! record_pid "$service" "$pid"; then
    START_SPAWNING=0
    return 1
  fi
  START_SPAWNING=0
  ((START_INTERRUPTED == 0)) || exit 130
}

load_pid_identity() {
  local file=$1
  local expected_pid=$2
  [[ -f "$file.identity" ]] || return 1
  read -r IDENTITY_PID IDENTITY_START IDENTITY_GROUP IDENTITY_SESSION <"$file.identity" || return 1
  [[ "$IDENTITY_PID" == "$expected_pid" \
    && "$IDENTITY_PID" =~ ^[0-9]+$ \
    && "$IDENTITY_START" =~ ^[0-9]+$ \
    && "$IDENTITY_GROUP" == "$IDENTITY_PID" \
    && "$IDENTITY_SESSION" == "$IDENTITY_PID" ]]
}

pid_identity_matches() {
  local pid=$1
  read_process_identity "$pid" || return 1
  [[ "$PROCESS_START" == "$IDENTITY_START" \
    && "$PROCESS_GROUP" == "$IDENTITY_GROUP" \
    && "$PROCESS_SESSION" == "$IDENTITY_SESSION" ]]
}

stop_pid() {
  local file=$1
  local timeout=${2:-10}
  [[ -f "$file" ]] || return 0
  local pid
  pid=$(<"$file")
  if [[ ! "$pid" =~ ^[0-9]+$ ]]; then
    printf 'harness: refusing to signal invalid pid record %s\n' "$file" >&2
    return 1
  fi
  if ! load_pid_identity "$file" "$pid"; then
    printf 'harness: refusing to signal incomplete identity record %s\n' "$file" >&2
    return 1
  fi
  if kill -0 "$pid" 2>/dev/null && ! pid_identity_matches "$pid"; then
    printf 'harness: refusing to signal reused pid %s from %s\n' "$pid" "$file" >&2
    return 1
  fi
  local group=$IDENTITY_GROUP
  local session=$IDENTITY_SESSION
  if ! session_has_live_processes "$session"; then
    rm -f "$file" "$file.identity"
    return 0
  fi

  local i
  if session_has_live_subgroups "$session" "$group"; then
    signal_session_subgroups "$session" "$group" TERM || return 1
    for ((i = 0; i < timeout * 10; i++)); do
      session_has_live_subgroups "$session" "$group" || break
      sleep 0.1 || true
    done
  fi

  if ! session_has_live_processes "$session"; then
    rm -f "$file" "$file.identity"
    return 0
  fi

  signal_session "$session" TERM || return 1
  for ((i = 0; i < 100; i++)); do
    if ! session_has_live_processes "$session"; then
      rm -f "$file" "$file.identity"
      return 0
    fi
    sleep 0.1 || true
  done
  signal_session "$session" KILL || return 1
  for ((i = 0; i < 20; i++)); do
    if ! session_has_live_processes "$session"; then
      rm -f "$file" "$file.identity"
      return 0
    fi
    sleep 0.1 || true
  done
  printf 'harness: process session %s did not stop\n' "$session" >&2
  return 1
}

require_instance_marker() {
  if [[ ! -f "$MARKER" ]]; then
    fail "refusing to operate on unmarked instance directory: $INSTANCE"
    return 1
  fi
  if [[ "$(<"$MARKER")" != "$HARNESS_ID" ]]; then
    fail "instance marker does not match $HARNESS_ID"
    return 1
  fi
}

write_manifest() {
  INSTANCE_ID="$HARNESS_ID" \
  INSTANCE_MODE="$HARNESS_MODE" \
  INSTANCE_ROOT="$INSTANCE" \
  API_URL="$API_URL" \
  FRONTEND_URL="$FRONTEND_URL" \
  FAKE_URL="$FAKE_URL" \
  API_PORT="$API_PORT" \
  FRONTEND_PORT="${UI_PORT:-}" \
  FAKE_PORT="$FAKE_PORT" \
  TEST_EMAIL="$TEST_EMAIL" \
  TEST_PASSWORD="$TEST_PASSWORD" \
  DB_PATH="$DB_PATH" \
  DATA_DIR="$DATA_DIR" \
  FS_ROOT="$FS_ROOT" \
  MOVIE_LIBRARY="$MOVIE_LIBRARY" \
  DOWNLOAD_DIR="$DOWNLOAD_DIR" \
  LOG_DIR="$LOG_DIR" \
  BACKEND_PID="$BACKEND_PID" \
  FRONTEND_PID="${FRONTEND_PID:-}" \
  FAKES_PID="$FAKES_PID" \
  node - "$MANIFEST" <<'NODE'
import { writeFileSync } from 'node:fs'

const manifest = {
  id: process.env.INSTANCE_ID,
  mode: process.env.INSTANCE_MODE,
  root: process.env.INSTANCE_ROOT,
  apiUrl: process.env.API_URL,
  frontendUrl: process.env.FRONTEND_URL,
  fakeUrl: process.env.FAKE_URL,
  ports: {
    backend: Number(process.env.API_PORT),
    ...(process.env.FRONTEND_PORT ? { frontend: Number(process.env.FRONTEND_PORT) } : {}),
    fakes: Number(process.env.FAKE_PORT),
  },
  credentials: {
    email: process.env.TEST_EMAIL,
    password: process.env.TEST_PASSWORD,
  },
  paths: {
    database: process.env.DB_PATH,
    data: process.env.DATA_DIR,
    libraryBase: process.env.FS_ROOT,
    movieLibrary: process.env.MOVIE_LIBRARY,
    downloads: process.env.DOWNLOAD_DIR,
    logs: process.env.LOG_DIR,
  },
  processes: {
    backend: Number(process.env.BACKEND_PID),
    ...(process.env.FRONTEND_PID ? { frontend: Number(process.env.FRONTEND_PID) } : {}),
    fakes: Number(process.env.FAKES_PID),
  },
}

writeFileSync(process.argv[2], `${JSON.stringify(manifest, null, 2)}\n`, { mode: 0o600 })
NODE
}

write_runtime_env() {
  {
    printf 'API_URL=%q\n' "$API_URL"
    printf 'FRONTEND_URL=%q\n' "$FRONTEND_URL"
    printf 'FAKE_URL=%q\n' "$FAKE_URL"
    printf 'API_PORT=%q\n' "$API_PORT"
    printf 'UI_PORT=%q\n' "${UI_PORT:-}"
    printf 'FAKE_PORT=%q\n' "$FAKE_PORT"
  } >"$INSTANCE/runtime.env"
  chmod 600 "$INSTANCE/runtime.env"
}

load_runtime_env() {
  [[ -f "$INSTANCE/runtime.env" ]] || fail "instance $HARNESS_ID does not exist"
  # shellcheck disable=SC1090
  source "$INSTANCE/runtime.env"
}

ensure_embedded_frontend() {
  if [[ -f "$ROOT/backend/frontend/dist/index.html" ]]; then
    return
  fi
  printf 'Preparing the backend embed used by the development build...\n'
  (cd "$ROOT/frontend" && npm run build-only)
  mkdir -p "$ROOT/backend/frontend/dist"
  cp -R "$ROOT/frontend/dist/." "$ROOT/backend/frontend/dist/"
}

prepare_binaries() {
  (cd "$ROOT/backend" && go build -o "$INSTANCE/bin/harness-fakes" ./cmd/harness-fakes/)
  (cd "$ROOT/backend" && go build -o "$INSTANCE/bin/harness-signal" ./cmd/harness-signal/)
  (cd "$ROOT/backend" && go build -o "$INSTANCE/bin/harness-supervisor" ./cmd/harness-supervisor/)
  if [[ "$HARNESS_MODE" == "local" ]]; then
    BACKEND_BINARY="$INSTANCE/bin/media-gate"
    (cd "$ROOT/backend" && go build -o "$BACKEND_BINARY" ./cmd/server/)
    return
  fi

  BACKEND_BINARY=${HARNESS_BINARY:-"$ROOT/media-gate"}
  if [[ -z "${HARNESS_BINARY:-}" ]]; then
    (cd "$ROOT" && make build)
  fi
  [[ -x "$BACKEND_BINARY" ]] || fail "harness binary is not executable: $BACKEND_BINARY"
}

configure_backend_env() {
  local port=$1
  BACKEND_ENV=(
    env -i
    PATH="$PATH"
    HOME="${HOME:-}"
    TMPDIR="${TMPDIR:-/tmp}"
    LANG="${LANG:-C.UTF-8}"
    MEDIAGATE_ENV_FILE=
    MEDIAGATE_API_HOST=127.0.0.1
    MEDIAGATE_API_PORT="$port"
    MEDIAGATE_BROWSER_OPEN=false
    MEDIAGATE_DATA_DIR="$DATA_DIR"
    MEDIAGATE_DB_PATH="$DB_PATH"
    MEDIAGATE_LIBRARY_BASEPATH="$FS_ROOT"
    MEDIAGATE_SECRET_KEY="$TEST_SECRET"
    MEDIAGATE_DEFAULTUSER_EMAIL="$TEST_EMAIL"
    MEDIAGATE_DEFAULTUSER_PASSWORD="$TEST_PASSWORD"
    MEDIAGATE_LOG_LEVEL=debug
    MEDIAGATE_LOG_FORMAT=json
    MEDIAGATE_TMDB_APIKEY="${HARNESS_TMDB_APIKEY:-}"
    MEDIAGATE_TVDB_APIKEY="${HARNESS_TVDB_APIKEY:-}"
  )
}

run_migrations() {
  configure_backend_env 0
  printf 'Running database migrations for harness instance %s...\n' "$HARNESS_ID"
  if ! timeout --signal=TERM --kill-after=5s 30s \
    "${BACKEND_ENV[@]}" "$BACKEND_BINARY" --migrate-only \
    </dev/null >>"$LOG_DIR/backend.log" 2>&1; then
    fail "database migration failed; inspect $LOG_DIR/backend.log"
  fi
}

start_fakes() {
  local binary="$INSTANCE/bin/harness-fakes"
  START_SPAWNING=1
  HARNESS_FAKE_PORT="$FAKE_PORT" setsid "$INSTANCE/bin/harness-supervisor" \
    "$binary" --addr "127.0.0.1:$FAKE_PORT" --download-root "$DOWNLOAD_DIR" \
    8>&- 9>&- </dev/null >>"$LOG_DIR/fakes.log" 2>&1 &
  FAKES_PID=$!
  finish_service_start fakes "$FAKES_PID"
  wait_http "$FAKE_URL/_harness/health" "fake integrations" 120 "$INSTANCE/pids/fakes.pid"
  pid_running "$INSTANCE/pids/fakes.pid" || fail "fake integration process exited during startup"
}

prepare_indexer_definition() {
  local definitions="$DATA_DIR/.cache/definitions"
  mkdir -p "$definitions"
  curl --silent --show-error --fail "$FAKE_URL/_harness/indexer.yml" \
    --max-time 5 \
    --output "$definitions/media-gate-harness.yml"
  date -u '+%Y-%m-%dT%H:%M:%SZ' >"$definitions/last_updated"
}

start_local_backend() {
  local air_config="$INSTANCE/air.toml"
  AIR_TMP="$INSTANCE/air" AIR_BIN="$INSTANCE/air/media-gate" node - "$air_config" <<'NODE'
import { writeFileSync } from 'node:fs'

const value = (text) => JSON.stringify(text)
const config = `root = "."
tmp_dir = ${value(process.env.AIR_TMP)}

[build]
  bin = ${value(process.env.AIR_BIN)}
  cmd = ${value('go build -o "$HARNESS_AIR_BIN" ./cmd/server/')}
  delay = 1000
  exclude_dir = ["tmp", "node_modules"]
  exclude_regex = ["_test\\\\.go$"]
  include_ext = ["go", "yaml"]
  kill_delay = 500

[log]
  time = false
`

writeFileSync(process.argv[2], config)
NODE
  START_SPAWNING=1
  setsid "$INSTANCE/bin/harness-supervisor" \
    bash -c 'cd "$1" && shift && exec "$@"' _ "$ROOT/backend" \
    "${BACKEND_ENV[@]}" \
    HARNESS_AIR_BIN="$INSTANCE/air/media-gate" \
    air -c "$air_config" 8>&- 9>&- </dev/null >>"$LOG_DIR/backend.log" 2>&1 &
  BACKEND_PID=$!
  finish_service_start backend "$BACKEND_PID"
}

start_ci_backend() {
  START_SPAWNING=1
  setsid "$INSTANCE/bin/harness-supervisor" "${BACKEND_ENV[@]}" "$BACKEND_BINARY" \
    8>&- 9>&- </dev/null >>"$LOG_DIR/backend.log" 2>&1 &
  BACKEND_PID=$!
  finish_service_start backend "$BACKEND_PID"
}

start_frontend() {
  START_SPAWNING=1
  setsid "$INSTANCE/bin/harness-supervisor" bash -c \
    'cd "$1" && shift && exec "$@"' _ "$ROOT/frontend" \
    env VITE_HOST=127.0.0.1 VITE_PORT="$UI_PORT" VITE_API_PROXY_TARGET="$API_URL" \
    npm run dev 8>&- 9>&- </dev/null >>"$LOG_DIR/frontend.log" 2>&1 &
  FRONTEND_PID=$!
  finish_service_start frontend "$FRONTEND_PID"
}

handle_start_signal() {
  START_INTERRUPTED=1
  ((START_SPAWNING == 1)) || exit 130
}

up() {
  validate_id
  [[ "$HARNESS_MODE" == "local" || "$HARNESS_MODE" == "ci" ]] || fail "HARNESS_MODE must be local or ci"
  require_command curl
  require_command go
  require_command node
  require_command setsid
  require_command flock
  require_command timeout

  if [[ "$HARNESS_MODE" == "local" ]]; then
    require_command air
    require_command npm
    [[ -d "$ROOT/frontend/node_modules" ]] || fail "frontend dependencies missing; run npm ci in frontend/"
    ensure_embedded_frontend
  fi

  if [[ -e "$INSTANCE" ]]; then
    if pid_running "$INSTANCE/pids/backend.pid" || pid_running "$INSTANCE/pids/fakes.pid"; then
      fail "instance $HARNESS_ID is already running"
    fi
    fail "instance directory already exists; run make harness-destroy HARNESS_ID=$HARNESS_ID"
  fi

  mkdir -p "$HARNESS_ROOT"
  if ! mkdir "$INSTANCE"; then
    fail "could not reserve harness instance $HARNESS_ID"
  fi
  printf '%s\n' "$HARNESS_ID" >"$MARKER"
  acquire_instance_lock
  acquire_startup_lock

  cleanup_failed_start() {
    local status=$?
    trap - EXIT
    trap '' HUP INT TERM
    down || true
    exit "$status"
  }
  trap cleanup_failed_start EXIT
  START_SPAWNING=0
  START_INTERRUPTED=0
  trap handle_start_signal HUP INT TERM

  TEST_EMAIL="harness@media-gate.test"
  TEST_PASSWORD="harness-password"
  TEST_SECRET="media-gate-harness-$HARNESS_ID-not-for-production"
  DB_PATH="$INSTANCE/media-gate.db"
  DATA_DIR="$INSTANCE/data"
  FS_ROOT="$INSTANCE/fs"
  MOVIE_LIBRARY="$FS_ROOT/library/movies"
  DOWNLOAD_DIR="$FS_ROOT/downloads"
  LOG_DIR="$INSTANCE/logs"

  mkdir -p "$INSTANCE/bin" "$INSTANCE/pids" "$LOG_DIR" "$DATA_DIR" "$MOVIE_LIBRARY/Harness Movie (2026)" "$DOWNLOAD_DIR"
  printf 'media-gate initial harness fixture\n' >"$MOVIE_LIBRARY/Harness Movie (2026)/Harness.Movie.2026.1080p.WEB-DL.mkv"

  prepare_binaries
  run_migrations
  allocate_ports
  API_URL="http://127.0.0.1:$API_PORT"
  FAKE_URL="http://127.0.0.1:$FAKE_PORT"
  if [[ "$HARNESS_MODE" == "local" ]]; then
    FRONTEND_URL="http://127.0.0.1:$UI_PORT"
  else
    FRONTEND_URL="$API_URL"
  fi
  configure_backend_env "$API_PORT"

  start_fakes
  prepare_indexer_definition
  if [[ "$HARNESS_MODE" == "local" ]]; then
    start_local_backend
    start_frontend
  else
    start_ci_backend
  fi

  write_runtime_env
  write_manifest

  wait_http "$API_URL/api/v1/setup/status" "backend" 120 "$INSTANCE/pids/backend.pid"
  pid_running "$INSTANCE/pids/backend.pid" || fail "backend process exited during startup"
  if [[ "$HARNESS_MODE" == "local" ]]; then
    wait_http "$FRONTEND_URL" "frontend" 120 "$INSTANCE/pids/frontend.pid"
    pid_running "$INSTANCE/pids/frontend.pid" || fail "frontend process exited during startup"
  fi
  if ! node "$ROOT/harness/seed.mjs" "$MANIFEST"; then
    fail "instance seed failed; inspect $LOG_DIR"
  fi
  release_startup_lock

  trap - EXIT HUP INT TERM

  printf 'Harness instance %s is ready.\n' "$HARNESS_ID"
  printf '  UI:       %s\n' "$FRONTEND_URL"
  printf '  API:      %s\n' "$API_URL"
  printf '  Fakes:    %s/_harness/state\n' "$FAKE_URL"
  printf '  Login:    %s / %s\n' "$TEST_EMAIL" "$TEST_PASSWORD"
  printf '  Manifest: %s\n' "$MANIFEST"
  printf '  Logs:     %s\n' "$LOG_DIR"
}

status() {
  validate_id
  load_runtime_env
  printf 'Harness instance %s\n' "$HARNESS_ID"
  local service
  for service in backend frontend fakes; do
    if pid_running "$INSTANCE/pids/$service.pid"; then
      printf '  %-9s running (pid %s)\n' "$service" "$(<"$INSTANCE/pids/$service.pid")"
    elif [[ -f "$INSTANCE/logs/$service.log" ]]; then
      printf '  %-9s stopped\n' "$service"
    fi
  done
  if curl --silent --fail --max-time 1 "$API_URL/api/v1/setup/status" >/dev/null 2>&1; then
    printf '  API       ready at %s\n' "$API_URL"
  else
    printf '  API       unavailable at %s\n' "$API_URL"
  fi
  printf '  UI        %s\n' "$FRONTEND_URL"
  printf '  Manifest  %s\n' "$MANIFEST"
}

logs() {
  validate_id
  local service=${1:-all}
  local files=()
  case "$service" in
    all)
      files=("$INSTANCE/logs/backend.log" "$INSTANCE/logs/frontend.log" "$INSTANCE/logs/fakes.log")
      ;;
    backend | frontend | fakes)
      files=("$INSTANCE/logs/$service.log")
      ;;
    *) fail "unknown service: $service" ;;
  esac

  local existing=()
  local file
  for file in "${files[@]}"; do
    [[ -f "$file" ]] && existing+=("$file")
  done
  ((${#existing[@]} > 0)) || fail "no logs found for instance $HARNESS_ID"
  if [[ "${FOLLOW:-0}" == "1" ]]; then
    tail -n 200 -F "${existing[@]}"
  else
    tail -n 200 "${existing[@]}"
  fi
}

control() {
  validate_id
  load_runtime_env
  curl --silent --show-error --fail --max-time 5 -X POST "$FAKE_URL/_harness/$1"
}

smoke() {
  validate_id
  [[ -f "$MANIFEST" ]] || fail "instance $HARNESS_ID does not exist"
  node "$ROOT/harness/smoke.mjs" "$MANIFEST"
}

down() {
  validate_id || return 1
  [[ -d "$INSTANCE" ]] || return 0
  require_instance_marker || return 1
  require_command flock || return 1
  acquire_instance_lock || return 1
  local failed=0
  stop_pid "$INSTANCE/pids/frontend.pid" 10 || failed=1
  stop_pid "$INSTANCE/pids/backend.pid" 40 || failed=1
  stop_pid "$INSTANCE/pids/fakes.pid" 10 || failed=1
  ((failed == 0)) || return 1
  printf 'Harness instance %s stopped; data retained at %s\n' "$HARNESS_ID" "$INSTANCE"
}

destroy() {
  validate_id || return 1
  [[ -d "$INSTANCE" ]] || return 0
  require_instance_marker || return 1
  require_command flock || return 1
  acquire_instance_lock || return 1
  down || return 1
  rm -rf -- "$INSTANCE" || return 1
  printf 'Harness instance %s destroyed.\n' "$HARNESS_ID"
}

handle_stop_signal() {
  STOP_INTERRUPTED=1
  trap '' HUP INT TERM
}

ci() {
  if [[ "$HARNESS_ID" == "default" ]]; then
    HARNESS_ID="ci-$$"
    INSTANCE="$HARNESS_ROOT/$HARNESS_ID"
    MANIFEST="$INSTANCE/manifest.json"
    MARKER="$INSTANCE/.media-gate-harness"
  fi
  HARNESS_MODE=ci
  local passed=0
  up
  cleanup_ci() {
    local status=$?
    trap - EXIT
    trap '' HUP INT TERM
    down || true
    printf 'Harness CI stopped; artifacts retained at %s\n' "$INSTANCE" >&2
    exit "$status"
  }
  trap cleanup_ci EXIT
  trap 'exit 130' HUP INT TERM
  if smoke; then
    passed=1
  fi
  down
  if [[ "$passed" == "1" ]]; then
    require_instance_marker
    rm -rf -- "$INSTANCE"
    trap - EXIT HUP INT TERM
    printf 'Harness CI smoke test passed.\n'
    return
  fi
  trap - EXIT HUP INT TERM
  printf 'Harness CI smoke test failed; artifacts retained at %s\n' "$INSTANCE" >&2
  return 1
}

command=${1:-}
case "$command" in
  up) up ;;
  status) status ;;
  logs) logs "${2:-all}" ;;
  smoke) smoke ;;
  complete) control complete ;;
  error) control error ;;
  reset-fakes) control reset ;;
  down)
    STOP_INTERRUPTED=0
    trap handle_stop_signal HUP INT TERM
    down
    trap - HUP INT TERM
    ((STOP_INTERRUPTED == 0)) || exit 130
    ;;
  destroy)
    STOP_INTERRUPTED=0
    trap handle_stop_signal HUP INT TERM
    destroy
    trap - HUP INT TERM
    ((STOP_INTERRUPTED == 0)) || exit 130
    ;;
  ci) ci ;;
  help | --help | -h) usage ;;
  *) usage; exit 1 ;;
esac
