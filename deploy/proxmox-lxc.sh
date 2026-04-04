#!/usr/bin/env bash
# deploy/proxmox-lxc.sh — Create a Proxmox LXC container for Media Gate
#
# Run on the Proxmox host:
#   bash deploy/proxmox-lxc.sh
#
# Prerequisites:
#   - Proxmox VE with pct/pvesh available
#   - Internet access (to download Debian template + GitHub release)
#   - A GitHub Personal Access Token (fine-grained, read-only Contents)

set -euo pipefail

# ═══════════════════════════════════════════════════════════════════════════════
# Helpers
# ═══════════════════════════════════════════════════════════════════════════════

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

info()  { echo -e "${CYAN}[INFO]${NC}  $*"; }
ok()    { echo -e "${GREEN}[OK]${NC}    $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
err()   { echo -e "${RED}[ERROR]${NC} $*" >&2; }
die()   { err "$@"; exit 1; }

prompt() {
    local var_name="$1" prompt_text="$2" default="${3:-}"
    local input
    if [[ -n "$default" ]]; then
        read -rp "$(echo -e "${BOLD}${prompt_text}${NC} [${default}]: ")" input </dev/tty
        eval "$var_name=\"${input:-$default}\""
    else
        read -rp "$(echo -e "${BOLD}${prompt_text}${NC}: ")" input </dev/tty
        eval "$var_name=\"$input\""
    fi
}

prompt_secret() {
    local var_name="$1" prompt_text="$2"
    local input
    read -srp "$(echo -e "${BOLD}${prompt_text}${NC}: ")" input </dev/tty
    echo
    eval "$var_name=\"$input\""
}

confirm() {
    local prompt_text="$1" default="${2:-N}"
    local yn
    read -rp "$(echo -e "${BOLD}${prompt_text}${NC} [y/N]: ")" yn </dev/tty
    [[ "${yn,,}" == "y" ]]
}

# ═══════════════════════════════════════════════════════════════════════════════
# Pre-flight checks
# ═══════════════════════════════════════════════════════════════════════════════

command -v pct    >/dev/null 2>&1 || die "pct not found — this script must run on a Proxmox VE host."
command -v pvesh  >/dev/null 2>&1 || die "pvesh not found — this script must run on a Proxmox VE host."
command -v curl   >/dev/null 2>&1 || die "curl not found."
command -v jq     >/dev/null 2>&1 || die "jq not found. Install with: apt install -y jq"

# ═══════════════════════════════════════════════════════════════════════════════
# Interactive configuration
# ═══════════════════════════════════════════════════════════════════════════════

echo
echo -e "${BOLD}═══ Media Gate — Proxmox LXC Deploy ═══${NC}"
echo

# --- LXC settings ---
echo -e "${BOLD}── LXC Container Settings ──${NC}"
NEXT_ID=$(pvesh get /cluster/nextid 2>/dev/null || echo "100")
prompt CTID        "Container ID"           "$NEXT_ID"
prompt HOSTNAME    "Hostname"               "media-gate"
prompt STORAGE     "Storage"                "local-lvm"
prompt DISK_SIZE   "Disk size (GB)"         "4"
prompt RAM         "RAM (MB)"               "512"
prompt SWAP        "Swap (MB)"              "256"
prompt CORES       "CPU cores"              "2"
prompt BRIDGE      "Network bridge"         "vmbr0"
prompt NET_CONFIG  "IP (dhcp or ip/cidr,gw=x.x.x.x)" "dhcp"
prompt_secret ROOT_PASS "Root password for the LXC container"
[[ -z "$ROOT_PASS" ]] && die "Root password is required."

# --- GitHub settings ---
echo
echo -e "${BOLD}── GitHub Release Settings ──${NC}"
prompt    GH_REPO  "GitHub repo (owner/repo)" "sumia01/media-gate"
prompt    GH_TOKEN "GitHub Personal Access Token (fine-grained, read-only Contents)"
[[ -z "$GH_TOKEN" ]] && die "GitHub token is required."
prompt    GH_TAG   "Release tag (latest or vX.Y.Z)" "latest"

# --- Migration / Secret key ---
echo
echo -e "${BOLD}── Database & Security ──${NC}"
MIGRATE_DB=false
if confirm "Migrate an existing database from a previous installation?"; then
    MIGRATE_DB=true
    prompt    OLD_DB_PATH  "Path to the DB file on this Proxmox host"
    [[ -f "$OLD_DB_PATH" ]] || die "File not found: ${OLD_DB_PATH}"
    prompt_secret SECRET_KEY "Secret key from the old installation (required)"
    [[ -z "$SECRET_KEY" ]] && die "Secret key is required when migrating an existing database."
else
    prompt SECRET_KEY "Secret key for encryption (leave empty to auto-generate)" ""
    if [[ -z "$SECRET_KEY" ]]; then
        SECRET_KEY=$(openssl rand -hex 32)
        info "Auto-generated secret key."
    fi
fi

# --- CIFS mount ---
echo
SETUP_CIFS=false
if confirm "Set up a NAS CIFS mount inside the LXC?"; then
    SETUP_CIFS=true
    echo
    echo -e "${BOLD}── NAS CIFS Settings ──${NC}"
    prompt        NAS_IP      "NAS IP or hostname"
    prompt        NAS_SHARE   "Share name"
    prompt        NAS_MOUNT   "Mount point inside LXC"  "/mnt/media"
    prompt        NAS_USER    "NAS username"
    prompt_secret NAS_PASS    "NAS password"
fi

# ═══════════════════════════════════════════════════════════════════════════════
# Resolve release asset URL
# ═══════════════════════════════════════════════════════════════════════════════

info "Fetching release info from GitHub..."

if [[ "$GH_TAG" == "latest" ]]; then
    RELEASE_URL="https://api.github.com/repos/${GH_REPO}/releases/latest"
else
    RELEASE_URL="https://api.github.com/repos/${GH_REPO}/releases/tags/${GH_TAG}"
fi

RELEASE_JSON=$(curl -fsSL -H "Authorization: token ${GH_TOKEN}" "$RELEASE_URL") \
    || die "Failed to fetch release. Check token permissions and repo name."

ASSET_URL=$(echo "$RELEASE_JSON" | jq -r '.assets[] | select(.name == "media-gate-linux-amd64") | .url') \
    || die "Could not find media-gate-linux-amd64 asset in the release."
RELEASE_TAG=$(echo "$RELEASE_JSON" | jq -r '.tag_name')

[[ -z "$ASSET_URL" || "$ASSET_URL" == "null" ]] && die "media-gate-linux-amd64 asset not found in release ${GH_TAG}."

ok "Found release ${RELEASE_TAG} with linux-amd64 binary."

# ═══════════════════════════════════════════════════════════════════════════════
# Phase 1: Create LXC container
# ═══════════════════════════════════════════════════════════════════════════════

echo
info "Setting up Debian 12 template..."

TEMPLATE_STORAGE="local"
TEMPLATE_NAME=$(pveam available --section system 2>/dev/null | grep -oP 'debian-12-standard_[^\s]+' | sort -V | tail -1)

if [[ -z "$TEMPLATE_NAME" ]]; then
    die "Could not find a Debian 12 template. Run: pveam update"
fi

# Download template if not already cached
if ! pveam list "$TEMPLATE_STORAGE" 2>/dev/null | grep -q "$TEMPLATE_NAME"; then
    info "Downloading ${TEMPLATE_NAME}..."
    pveam download "$TEMPLATE_STORAGE" "$TEMPLATE_NAME" || die "Template download failed."
fi
ok "Template ready: ${TEMPLATE_NAME}"

info "Creating LXC container ${CTID} (${HOSTNAME})..."

if [[ "$NET_CONFIG" == "dhcp" ]]; then
    NET_STRING="name=eth0,bridge=${BRIDGE},ip=dhcp"
else
    NET_STRING="name=eth0,bridge=${BRIDGE},ip=${NET_CONFIG}"
fi

LXC_FEATURES="nesting=1"
if [[ "$SETUP_CIFS" == true ]]; then
    LXC_FEATURES="nesting=1,mount=cifs"
fi

pct create "$CTID" "${TEMPLATE_STORAGE}:vztmpl/${TEMPLATE_NAME}" \
    --hostname "$HOSTNAME" \
    --storage "$STORAGE" \
    --rootfs "${STORAGE}:${DISK_SIZE}" \
    --memory "$RAM" \
    --swap "$SWAP" \
    --cores "$CORES" \
    --net0 "$NET_STRING" \
    --unprivileged 1 \
    --features "$LXC_FEATURES" \
    --password "$ROOT_PASS" \
    --start 0 \
    || die "pct create failed."

ok "Container ${CTID} created."

info "Starting container..."
pct start "$CTID" || die "Failed to start container ${CTID}."

# Wait for container to be fully up
sleep 3

ok "Container ${CTID} is running."

# ═══════════════════════════════════════════════════════════════════════════════
# Helper: run command inside LXC
# ═══════════════════════════════════════════════════════════════════════════════

lxc_exec() {
    pct exec "$CTID" -- bash -c "$1"
}

# ═══════════════════════════════════════════════════════════════════════════════
# Phase 2: Setup inside LXC
# ═══════════════════════════════════════════════════════════════════════════════

info "Installing packages inside LXC..."
lxc_exec "apt-get update -qq && apt-get install -y -qq curl jq > /dev/null 2>&1" \
    || die "Failed to install packages."
ok "Packages installed."

info "Creating mediagate user and directories..."
lxc_exec "
    useradd --system --shell /usr/sbin/nologin --home-dir /var/lib/media-gate mediagate 2>/dev/null || true
    mkdir -p /opt/media-gate
    mkdir -p /var/lib/media-gate/.cache/posters
    mkdir -p /var/lib/media-gate/.cache/definitions
    mkdir -p /etc/media-gate
    chown -R mediagate:mediagate /var/lib/media-gate
"
ok "User and directories created."

info "Downloading Media Gate ${RELEASE_TAG}..."
lxc_exec "
    curl -fsSL \
        -H 'Authorization: token ${GH_TOKEN}' \
        -H 'Accept: application/octet-stream' \
        '${ASSET_URL}' \
        -o /opt/media-gate/media-gate \
    && chmod +x /opt/media-gate/media-gate \
    && echo '${RELEASE_TAG}' > /opt/media-gate/VERSION
" || die "Failed to download binary."
ok "Binary installed."

info "Writing configuration..."

# Determine library base path
LIBRARY_BASEPATH="/mnt"
if [[ "$SETUP_CIFS" == true ]]; then
    LIBRARY_BASEPATH="$NAS_MOUNT"
fi

lxc_exec "
cat > /etc/default/media-gate <<'ENVEOF'
# Media Gate configuration — managed by deploy script
MEDIAGATE_SECRET_KEY=${SECRET_KEY}
MEDIAGATE_DB_PATH=/var/lib/media-gate/media-gate.db
MEDIAGATE_API_PORT=8080
MEDIAGATE_LIBRARY_BASEPATH=${LIBRARY_BASEPATH}
MEDIAGATE_LOG_LEVEL=info
MEDIAGATE_LOG_FORMAT=text
ENVEOF
chmod 600 /etc/default/media-gate
"

# Save GitHub config for the update script
lxc_exec "
cat > /etc/media-gate/github.conf <<'GHEOF'
GH_REPO=${GH_REPO}
GH_TOKEN=${GH_TOKEN}
GHEOF
chmod 600 /etc/media-gate/github.conf
"
ok "Configuration written."

# ── systemd service ─────────────────────────────────────────────────────────

info "Installing systemd service..."

AFTER_TARGETS="network-online.target"
RW_PATHS="/var/lib/media-gate"

if [[ "$SETUP_CIFS" == true ]]; then
    AFTER_TARGETS="network-online.target remote-fs.target"
    RW_PATHS="/var/lib/media-gate ${NAS_MOUNT}"
fi

lxc_exec "
cat > /etc/systemd/system/media-gate.service <<'SVCEOF'
[Unit]
Description=Media Gate
After=${AFTER_TARGETS}
Wants=network-online.target

[Service]
Type=simple
User=mediagate
Group=mediagate
WorkingDirectory=/var/lib/media-gate
EnvironmentFile=/etc/default/media-gate
ExecStart=/opt/media-gate/media-gate
Restart=on-failure
RestartSec=5

NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=${RW_PATHS}
PrivateTmp=true

[Install]
WantedBy=multi-user.target
SVCEOF

systemctl daemon-reload
"
ok "Systemd service installed."

# ── update script ───────────────────────────────────────────────────────────

info "Installing update script..."

lxc_exec '
cat > /usr/local/bin/media-gate-update <<'"'"'UPDEOF'"'"'
#!/usr/bin/env bash
set -euo pipefail

source /etc/media-gate/github.conf

TAG="${1:-latest}"

if [[ "$TAG" == "latest" ]]; then
    URL="https://api.github.com/repos/${GH_REPO}/releases/latest"
else
    URL="https://api.github.com/repos/${GH_REPO}/releases/tags/${TAG}"
fi

echo "Fetching release info..."
RELEASE=$(curl -fsSL -H "Authorization: token ${GH_TOKEN}" "$URL")
ASSET_URL=$(echo "$RELEASE" | jq -r '"'"'.assets[] | select(.name == "media-gate-linux-amd64") | .url'"'"')
NEW_TAG=$(echo "$RELEASE" | jq -r '"'"'.tag_name'"'"')

if [[ -z "$ASSET_URL" || "$ASSET_URL" == "null" ]]; then
    echo "ERROR: media-gate-linux-amd64 not found in release."
    exit 1
fi

CURRENT=""
[[ -f /opt/media-gate/VERSION ]] && CURRENT=$(cat /opt/media-gate/VERSION)

if [[ "$CURRENT" == "$NEW_TAG" ]]; then
    echo "Already up to date (${CURRENT})."
    exit 0
fi

echo "Updating: ${CURRENT:-unknown} -> ${NEW_TAG}"
echo "Downloading..."
curl -fsSL \
    -H "Authorization: token ${GH_TOKEN}" \
    -H "Accept: application/octet-stream" \
    "$ASSET_URL" \
    -o /tmp/media-gate-new

echo "Stopping service..."
systemctl stop media-gate

mv /tmp/media-gate-new /opt/media-gate/media-gate
chmod +x /opt/media-gate/media-gate
echo "$NEW_TAG" > /opt/media-gate/VERSION

echo "Starting service..."
systemctl start media-gate

echo "Updated to ${NEW_TAG}."
systemctl --no-pager status media-gate
UPDEOF
chmod +x /usr/local/bin/media-gate-update
'
ok "Update script installed (/usr/local/bin/media-gate-update)."

# ── DB migration ────────────────────────────────────────────────────────────

if [[ "$MIGRATE_DB" == true ]]; then
    info "Migrating database..."
    pct push "$CTID" "$OLD_DB_PATH" /var/lib/media-gate/media-gate.db \
        || die "Failed to copy database into container."
    lxc_exec "chown mediagate:mediagate /var/lib/media-gate/media-gate.db"
    ok "Database migrated from ${OLD_DB_PATH}."
fi

# ═══════════════════════════════════════════════════════════════════════════════
# Phase 2.5: Optional CIFS mount
# ═══════════════════════════════════════════════════════════════════════════════

if [[ "$SETUP_CIFS" == true ]]; then
    info "Setting up CIFS mount..."

    lxc_exec "apt-get install -y -qq cifs-utils > /dev/null 2>&1" \
        || die "Failed to install cifs-utils."

    lxc_exec "
cat > /etc/cifs-credentials <<'CIFSEOF'
username=${NAS_USER}
password=${NAS_PASS}
CIFSEOF
chmod 600 /etc/cifs-credentials

mkdir -p '${NAS_MOUNT}'

# Add fstab entry if not already present
if ! grep -q '${NAS_MOUNT}' /etc/fstab; then
    echo '//${NAS_IP}/${NAS_SHARE} ${NAS_MOUNT} cifs credentials=/etc/cifs-credentials,uid=mediagate,gid=mediagate,iocharset=utf8,file_mode=0775,dir_mode=0775,nofail 0 0' >> /etc/fstab
fi

mount -a
"

    # Verify mount
    if lxc_exec "mountpoint -q '${NAS_MOUNT}'"; then
        ok "CIFS mount active at ${NAS_MOUNT}."
    else
        warn "CIFS mount at ${NAS_MOUNT} could not be verified. Check credentials and network."
    fi
fi

# ═══════════════════════════════════════════════════════════════════════════════
# Phase 3: Start service and print summary
# ═══════════════════════════════════════════════════════════════════════════════

info "Starting Media Gate service..."
lxc_exec "systemctl enable --now media-gate" || die "Failed to start service."

# Give it a moment to start
sleep 2

SERVICE_STATUS=$(lxc_exec "systemctl is-active media-gate" || true)
CONTAINER_IP=$(lxc_exec "hostname -I | awk '{print \$1}'" || echo "unknown")

echo
echo -e "${BOLD}═══════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}${BOLD} Media Gate deployed successfully!${NC}"
echo -e "${BOLD}═══════════════════════════════════════════════════════${NC}"
echo
echo -e "  Container ID:   ${CYAN}${CTID}${NC}"
echo -e "  Hostname:       ${CYAN}${HOSTNAME}${NC}"
echo -e "  IP Address:     ${CYAN}${CONTAINER_IP}${NC}"
echo -e "  Service:        ${CYAN}${SERVICE_STATUS}${NC}"
echo -e "  Version:        ${CYAN}${RELEASE_TAG}${NC}"
echo
echo -e "  ${BOLD}Secret Key:${NC}       ${YELLOW}${SECRET_KEY}${NC}"
if [[ "$MIGRATE_DB" != true ]]; then
    echo -e "                    ${YELLOW}Save this! It encrypts all secrets in the DB.${NC}"
    echo -e "                    ${YELLOW}Without it, encrypted settings cannot be recovered.${NC}"
fi
echo
echo -e "  ${BOLD}Open in browser:${NC}  http://${CONTAINER_IP}:8080"
echo -e "  ${BOLD}Update command:${NC}   pct exec ${CTID} -- media-gate-update"
echo -e "  ${BOLD}Service logs:${NC}     pct exec ${CTID} -- journalctl -u media-gate -f"
echo
if [[ "$SETUP_CIFS" == true ]]; then
    echo -e "  ${BOLD}NAS mount:${NC}        ${NAS_MOUNT} -> //${NAS_IP}/${NAS_SHARE}"
    echo
fi
if [[ "$MIGRATE_DB" == true ]]; then
    echo -e "  Database migrated — the setup wizard will be skipped."
else
    echo -e "  The setup wizard will guide you through initial configuration."
fi
echo
