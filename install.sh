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

install_service() {
  log "Installing background service…"
  # The binary writes the native unit file (launchd on macOS, systemd --user
  # on Linux) and starts it.
  "${INSTALL_DIR}/${BIN_NAME}" install
}

main() {
  command -v curl >/dev/null || die "curl is required"
  local platform backend
  platform="$(detect_platform)"
  log "Platform: ${platform}"
  download_binary "$platform"
  backend="$(prompt_backend)"
  write_settings "$backend"
  install_service
  cat <<EOF

\033[32m✔\033[0m supervAIsor poller installed.

  Binary:   ${INSTALL_DIR}/${BIN_NAME}
  Settings: ${SETTINGS_FILE}
  Logs:     ${CONFIG_DIR}/poller.log

Service commands (managed by $(uname -s | tr A-Z a-z | sed 's/darwin/launchd/;s/linux/systemd --user/')):
  supervaisor status
  supervaisor start
  supervaisor stop
  supervaisor restart
  supervaisor uninstall

Make sure ${INSTALL_DIR} is in your PATH.
EOF
}

main "$@"
