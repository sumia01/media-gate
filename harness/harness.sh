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
  mkdir -p "$HARNESS_ROOT/.locks"
  exec 9>"$HARNESS_ROOT/.locks/$HARNESS_ID.lock"
  flock -n 9 || fail "another lifecycle command is running for instance $HARNESS_ID"
  INSTANCE_LOCKED=1
}

acquire_startup_lock() {
  exec 8>"$HARNESS_ROOT/.startup.lock"
  flock 8
}

release_startup_lock() {
  flock -u 8
  exec 8>&-
}

port_available() {
  ! (exec 3<>"/dev/tcp/127.0.0.1/$1") >/dev/null 2>&1
}

pick_port() {
  local start=$1
  local port
  for ((port = start; port < start + 500; port++)); do
    if port_available "$port"; then
      printf '%s\n' "$port"
      return
    fi
  done
  fail "no free port found from $start"
}

wait_http() {
  local url=$1
  local label=$2
  local attempts=${3:-120}
  local i
  for ((i = 0; i < attempts; i++)); do
    if curl --silent --show-error --fail --max-time 1 "$url" >/dev/null 2>&1; then
      return
    fi
    sleep 0.25
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
  if kill -0 "$pid" 2>/dev/null && ! pid_identity_matches "$file" "$pid"; then
    return 1
  fi
  kill -0 -- "-$pid" 2>/dev/null || kill -0 "$pid" 2>/dev/null
}

process_start_time() {
  local pid=$1
  [[ -r "/proc/$pid/stat" ]] || return 1
  local stat fields
  stat=$(<"/proc/$pid/stat")
  stat=${stat#*) }
  read -r -a fields <<<"$stat"
  ((${#fields[@]} >= 20)) || return 1
  printf '%s\n' "${fields[19]}"
}

record_pid() {
  local service=$1
  local pid=$2
  local file="$INSTANCE/pids/$service.pid"
  printf '%s\n' "$pid" >"$file"
  local started
  if started=$(process_start_time "$pid"); then
    printf '%s\n' "$started" >"$file.start"
  fi
}

pid_identity_matches() {
  local file=$1
  local pid=$2
  [[ -f "$file.start" ]] || return 0
  local current expected
  expected=$(<"$file.start")
  current=$(process_start_time "$pid") || return 1
  [[ "$current" == "$expected" ]]
}

stop_pid() {
  local file=$1
  local timeout=${2:-10}
  [[ -f "$file" ]] || return 0
  local pid
  pid=$(<"$file")
  if [[ ! "$pid" =~ ^[0-9]+$ ]]; then
    rm -f "$file" "$file.start"
    return 0
  fi
  if kill -0 "$pid" 2>/dev/null && ! pid_identity_matches "$file" "$pid"; then
    printf 'harness: refusing to signal reused pid %s from %s\n' "$pid" "$file" >&2
    rm -f "$file" "$file.start"
    return 0
  fi
  if ! kill -0 -- "-$pid" 2>/dev/null && ! kill -0 "$pid" 2>/dev/null; then
    rm -f "$file" "$file.start"
    return 0
  fi

  kill -TERM -- "-$pid" 2>/dev/null || kill -TERM "$pid" 2>/dev/null || true
  local i
  for ((i = 0; i < timeout * 10; i++)); do
    if ! kill -0 -- "-$pid" 2>/dev/null && ! kill -0 "$pid" 2>/dev/null; then
      rm -f "$file" "$file.start"
      return 0
    fi
    sleep 0.1
  done
  kill -KILL -- "-$pid" 2>/dev/null || kill -KILL "$pid" 2>/dev/null || true
  for ((i = 0; i < 20; i++)); do
    if ! kill -0 -- "-$pid" 2>/dev/null && ! kill -0 "$pid" 2>/dev/null; then
      rm -f "$file" "$file.start"
      return 0
    fi
    sleep 0.1
  done
  printf 'harness: process group %s did not stop\n' "$pid" >&2
  return 1
}

require_instance_marker() {
  [[ -f "$MARKER" ]] || fail "refusing to operate on unmarked instance directory: $INSTANCE"
  [[ "$(<"$MARKER")" == "$HARNESS_ID" ]] || fail "instance marker does not match $HARNESS_ID"
}

write_manifest() {
  INSTANCE_ID="$HARNESS_ID" \
  INSTANCE_MODE="$HARNESS_MODE" \
  INSTANCE_ROOT="$INSTANCE" \
  API_URL="$API_URL" \
  FRONTEND_URL="$FRONTEND_URL" \
  FAKE_URL="$FAKE_URL" \
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

start_fakes() {
  local binary="$INSTANCE/bin/harness-fakes"
  (cd "$ROOT/backend" && go build -o "$binary" ./cmd/harness-fakes/)
  setsid "$binary" --addr "127.0.0.1:$FAKE_PORT" --download-root "$DOWNLOAD_DIR" \
    8>&- 9>&- </dev/null >>"$LOG_DIR/fakes.log" 2>&1 &
  FAKES_PID=$!
  record_pid fakes "$FAKES_PID"
  wait_http "$FAKE_URL/_harness/health" "fake integrations"
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
  setsid bash -c 'cd "$1" && shift && exec "$@"' _ "$ROOT/backend" \
    env -i \
    PATH="$PATH" \
    HOME="${HOME:-}" \
    TMPDIR="${TMPDIR:-/tmp}" \
    LANG="${LANG:-C.UTF-8}" \
    MEDIAGATE_ENV_FILE= \
    MEDIAGATE_API_HOST=127.0.0.1 \
    MEDIAGATE_API_PORT="$API_PORT" \
    MEDIAGATE_BROWSER_OPEN=false \
    MEDIAGATE_DATA_DIR="$DATA_DIR" \
    MEDIAGATE_DB_PATH="$DB_PATH" \
    MEDIAGATE_LIBRARY_BASEPATH="$FS_ROOT" \
    MEDIAGATE_SECRET_KEY="$TEST_SECRET" \
    MEDIAGATE_DEFAULTUSER_EMAIL="$TEST_EMAIL" \
    MEDIAGATE_DEFAULTUSER_PASSWORD="$TEST_PASSWORD" \
    MEDIAGATE_LOG_LEVEL=debug \
    MEDIAGATE_LOG_FORMAT=json \
    MEDIAGATE_TMDB_APIKEY="${HARNESS_TMDB_APIKEY:-}" \
    MEDIAGATE_TVDB_APIKEY="${HARNESS_TVDB_APIKEY:-}" \
    HARNESS_AIR_BIN="$INSTANCE/air/media-gate" \
    air -c "$air_config" 8>&- 9>&- </dev/null >>"$LOG_DIR/backend.log" 2>&1 &
  BACKEND_PID=$!
  record_pid backend "$BACKEND_PID"
}

start_ci_backend() {
  local binary=${HARNESS_BINARY:-"$ROOT/media-gate"}
  if [[ -z "${HARNESS_BINARY:-}" ]]; then
    (cd "$ROOT" && make build)
  fi
  [[ -x "$binary" ]] || fail "harness binary is not executable: $binary"
  setsid bash -c 'exec "$@"' _ \
    env -i \
    PATH="$PATH" \
    HOME="${HOME:-}" \
    TMPDIR="${TMPDIR:-/tmp}" \
    LANG="${LANG:-C.UTF-8}" \
    MEDIAGATE_ENV_FILE= \
    MEDIAGATE_API_HOST=127.0.0.1 \
    MEDIAGATE_API_PORT="$API_PORT" \
    MEDIAGATE_BROWSER_OPEN=false \
    MEDIAGATE_DATA_DIR="$DATA_DIR" \
    MEDIAGATE_DB_PATH="$DB_PATH" \
    MEDIAGATE_LIBRARY_BASEPATH="$FS_ROOT" \
    MEDIAGATE_SECRET_KEY="$TEST_SECRET" \
    MEDIAGATE_DEFAULTUSER_EMAIL="$TEST_EMAIL" \
    MEDIAGATE_DEFAULTUSER_PASSWORD="$TEST_PASSWORD" \
    MEDIAGATE_LOG_LEVEL=debug \
    MEDIAGATE_LOG_FORMAT=json \
    MEDIAGATE_TMDB_APIKEY="${HARNESS_TMDB_APIKEY:-}" \
    MEDIAGATE_TVDB_APIKEY="${HARNESS_TVDB_APIKEY:-}" \
    "$binary" 8>&- 9>&- </dev/null >>"$LOG_DIR/backend.log" 2>&1 &
  BACKEND_PID=$!
  record_pid backend "$BACKEND_PID"
}

start_frontend() {
  VITE_API_PROXY_TARGET="$API_URL" setsid bash -c \
    'cd "$1" && exec npm run dev -- --host 127.0.0.1 --port "$2" --strictPort' \
    _ "$ROOT/frontend" "$UI_PORT" 8>&- 9>&- </dev/null >>"$LOG_DIR/frontend.log" 2>&1 &
  FRONTEND_PID=$!
  record_pid frontend "$FRONTEND_PID"
}

up() {
  validate_id
  [[ "$HARNESS_MODE" == "local" || "$HARNESS_MODE" == "ci" ]] || fail "HARNESS_MODE must be local or ci"
  require_command curl
  require_command go
  require_command node
  require_command setsid
  require_command flock

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
    trap - ERR INT TERM
    down || true
    return "$status"
  }
  trap cleanup_failed_start ERR
  trap 'trap - ERR INT TERM; down || true; exit 130' INT TERM

  local checksum
  checksum=$(printf '%s' "$HARNESS_ID" | cksum)
  checksum=${checksum%% *}
  API_PORT=${HARNESS_API_PORT:-$(pick_port $((18080 + checksum % 300)))}
  FAKE_PORT=${HARNESS_FAKE_PORT:-$(pick_port $((20080 + checksum % 300)))}
  local port
  for port in "$API_PORT" "$FAKE_PORT"; do
    [[ "$port" =~ ^[0-9]+$ ]] && ((port > 0 && port < 65536)) || fail "invalid harness port: $port"
  done
  [[ "$API_PORT" != "$FAKE_PORT" ]] || fail "backend and fake ports must be distinct"
  port_available "$API_PORT" || fail "backend port is in use: $API_PORT"
  port_available "$FAKE_PORT" || fail "fake integration port is in use: $FAKE_PORT"

  API_URL="http://127.0.0.1:$API_PORT"
  FAKE_URL="http://127.0.0.1:$FAKE_PORT"
  if [[ "$HARNESS_MODE" == "local" ]]; then
    UI_PORT=${HARNESS_UI_PORT:-$(pick_port $((19080 + checksum % 300)))}
    [[ "$UI_PORT" =~ ^[0-9]+$ ]] && ((UI_PORT > 0 && UI_PORT < 65536)) || fail "invalid harness port: $UI_PORT"
    [[ "$UI_PORT" != "$API_PORT" && "$UI_PORT" != "$FAKE_PORT" ]] || fail "frontend port must be distinct"
    port_available "$UI_PORT" || fail "frontend port is in use: $UI_PORT"
    FRONTEND_URL="http://127.0.0.1:$UI_PORT"
  else
    FRONTEND_URL="$API_URL"
  fi
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

  if ! wait_http "$API_URL/api/v1/setup/status" "backend"; then
    down
    return 1
  fi
  pid_running "$INSTANCE/pids/backend.pid" || fail "backend process exited during startup"
  if ! wait_http "$FRONTEND_URL" "frontend"; then
    down
    return 1
  fi
  if [[ "$HARNESS_MODE" == "local" ]]; then
    pid_running "$INSTANCE/pids/frontend.pid" || fail "frontend process exited during startup"
  fi
  if ! node "$ROOT/harness/seed.mjs" "$MANIFEST"; then
    down
    fail "instance seed failed; inspect $LOG_DIR"
  fi
  release_startup_lock

  trap - ERR INT TERM

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
  validate_id
  [[ -d "$INSTANCE" ]] || return 0
  require_instance_marker
  require_command flock
  acquire_instance_lock
  local failed=0
  stop_pid "$INSTANCE/pids/frontend.pid" 10 || failed=1
  stop_pid "$INSTANCE/pids/backend.pid" 40 || failed=1
  stop_pid "$INSTANCE/pids/fakes.pid" 10 || failed=1
  ((failed == 0)) || return 1
  printf 'Harness instance %s stopped; data retained at %s\n' "$HARNESS_ID" "$INSTANCE"
}

destroy() {
  validate_id
  [[ -d "$INSTANCE" ]] || return 0
  require_instance_marker
  require_command flock
  acquire_instance_lock
  down
  rm -rf -- "$INSTANCE"
  printf 'Harness instance %s destroyed.\n' "$HARNESS_ID"
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
  trap 'trap - EXIT INT TERM; down || true' EXIT
  trap 'exit 130' INT TERM
  if smoke; then
    passed=1
  fi
  down
  trap - EXIT INT TERM
  if [[ "$passed" == "1" ]]; then
    require_instance_marker
    rm -rf -- "$INSTANCE"
    printf 'Harness CI smoke test passed.\n'
    return
  fi
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
  down) down ;;
  destroy) destroy ;;
  ci) ci ;;
  help | --help | -h) usage ;;
  *) usage; exit 1 ;;
esac
