#!/usr/bin/env bash
# supervAIsor poller installer.
#
# Downloads the latest poller binary from GitHub releases, materializes the
# settings file under ~/.supervaisor/, and launches it in the background.
# Re-running the script is safe: it stops the previous instance first.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/luxarts/supervAIsor/main/install.sh | bash
#   # or with a pre-set backend:
#   SUPERVAISOR_BACKEND=mmm4p.local/supervaisor bash install.sh

set -euo pipefail

REPO="luxarts/supervAIsor"
BIN_NAME="supervaisor"
INSTALL_DIR="${HOME}/.local/bin"
CONFIG_DIR="${HOME}/.supervaisor"
SETTINGS_FILE="${CONFIG_DIR}/settings.json"
LOG_FILE="${CONFIG_DIR}/poller.log"
PID_FILE="${CONFIG_DIR}/poller.pid"

log() { printf '\033[36m›\033[0m %s\n' "$*"; }
die() { printf '\033[31m✖\033[0m %s\n' "$*" >&2; exit 1; }

detect_platform() {
  local os arch
  case "$(uname -s)" in
    Darwin) os="darwin" ;;
    Linux)  os="linux" ;;
    *)      die "Unsupported OS: $(uname -s)" ;;
  esac
  case "$(uname -m)" in
    arm64|aarch64) arch="arm64" ;;
    x86_64|amd64)  arch="amd64" ;;
    *)             die "Unsupported arch: $(uname -m)" ;;
  esac
  echo "${os}-${arch}"
}

download_binary() {
  local platform="$1"
  local asset="${BIN_NAME}-${platform}"
  local url="https://github.com/${REPO}/releases/latest/download/${asset}"
  mkdir -p "$INSTALL_DIR"
  log "Downloading ${asset} from latest release…"
  if ! curl -fsSL -o "${INSTALL_DIR}/${BIN_NAME}" "$url"; then
    die "Download failed. Check that a release with asset '${asset}' exists at https://github.com/${REPO}/releases/latest"
  fi
  chmod +x "${INSTALL_DIR}/${BIN_NAME}"
  log "Installed to ${INSTALL_DIR}/${BIN_NAME}"
}

prompt_backend() {
  if [ -n "${SUPERVAISOR_BACKEND:-}" ]; then
    echo "$SUPERVAISOR_BACKEND"
    return
  fi
  local default="localhost:8080" answer
  printf '\033[36m?\033[0m Backend (host[:port][/path], no scheme) [%s]: ' "$default" >&2
  read -r answer </dev/tty || answer=""
  echo "${answer:-$default}"
}

write_settings() {
  local backend="$1"
  mkdir -p "$CONFIG_DIR"
  if [ -f "$SETTINGS_FILE" ]; then
    log "Settings file already exists at ${SETTINGS_FILE} — leaving it untouched"
    return
  fi
  cat >"$SETTINGS_FILE" <<EOF
{
  "projects_dir": "${HOME}/.claude/projects",
  "state_file":   "${CONFIG_DIR}/state.json",
  "backend":      "${backend}",
  "hostname":     "",
  "interval":     "1s"
}
EOF
  log "Wrote ${SETTINGS_FILE}"
}

stop_existing() {
  [ -f "$PID_FILE" ] || return 0
  local pid
  pid="$(cat "$PID_FILE" 2>/dev/null || true)"
  if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
    log "Stopping previous poller (pid $pid)…"
    kill "$pid" 2>/dev/null || true
    # Give it a beat to exit cleanly.
    for _ in 1 2 3 4 5; do
      kill -0 "$pid" 2>/dev/null || break
      sleep 0.2
    done
    kill -9 "$pid" 2>/dev/null || true
  fi
  rm -f "$PID_FILE"
}

start_background() {
  stop_existing
  log "Starting poller in background (logs: ${LOG_FILE})"
  nohup "${INSTALL_DIR}/${BIN_NAME}" >>"$LOG_FILE" 2>&1 &
  echo $! >"$PID_FILE"
  disown 2>/dev/null || true
  sleep 0.5
  if ! kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
    die "Poller exited immediately. Tail of log:\n$(tail -n 20 "$LOG_FILE")"
  fi
  log "Running (pid $(cat "$PID_FILE"))"
}

main() {
  command -v curl >/dev/null || die "curl is required"
  local platform backend
  platform="$(detect_platform)"
  log "Platform: ${platform}"
  download_binary "$platform"
  backend="$(prompt_backend)"
  write_settings "$backend"
  start_background
  cat <<EOF

\033[32m✔\033[0m supervAIsor poller installed.

  Binary:   ${INSTALL_DIR}/${BIN_NAME}
  Settings: ${SETTINGS_FILE}
  Logs:     ${LOG_FILE}
  PID:      $(cat "$PID_FILE")

To stop:    kill \$(cat ${PID_FILE})
To restart: bash <(curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh)
EOF
}

main "$@"
