#!/bin/bash
# Master Multi-OS Matrix Test Runner for M.U.T.A.T.E.
# Spawns isolated environments (containers/VMs) across Debian, Ubuntu, Fedora, Arch Linux, Alpine
# Tests installation via `curl ... | sh -s -- --system` and verifies all 7 package formats.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
FIXTURES_DIR="${ROOT_DIR}/tests/fixtures"

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m'

say() { printf "${BLUE}${BOLD}==>${NC} %s\n" "$*"; }
success() { printf "${GREEN}${BOLD}✓${NC} %s\n" "$*"; }
warn() { printf "${YELLOW}${BOLD}!${NC} %s\n" "$*"; }
error() { printf "${RED}${BOLD}✗${NC} %s\n" "$*"; exit 1; }

# Parse arguments
TARGET_OS="all"
MODE="online" # online or local

usage() {
    cat <<EOF
Usage: $0 [OPTIONS]

Options:
  --os <name>        Select OS to test: all (default), alpine, debian, ubuntu, fedora, arch
  --mode <mode>      Installation mode:
                       online (default) - uses 'curl -fsSL https://raw.githubusercontent.com/rfdetoni/mutate/main/install.sh | sh -s -- --system'
                       local           - uses local prebuilt package/installer for fast offline execution
  -h, --help         Show this help message
EOF
    exit 0
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --os) TARGET_OS="$2"; shift 2 ;;
        --mode) MODE="$2"; shift 2 ;;
        -h|--help) usage ;;
        *) error "Unknown argument: $1" ;;
    esac
done

# Check container runtime
RUNTIME=""
if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
    RUNTIME="docker"
elif command -v podman >/dev/null 2>&1 && podman info >/dev/null 2>&1; then
    RUNTIME="podman"
else
    error "Docker or Podman runtime is required to execute OS matrix tests."
fi
say "Using container engine: ${RUNTIME}"

# Step 1: Ensure test fixtures exist
say "Checking package fixtures in ${FIXTURES_DIR}..."
REQUIRED_FIXTURES=("sample.deb" "sample.rpm" "sample.pkg.tar.zst" "sample-alpine.apk" "sample-android.apk" "sample-source.tar.gz" "sample.AppImage")
MISSING_FIXTURES=0

for fix in "${REQUIRED_FIXTURES[@]}"; do
    if [ ! -f "${FIXTURES_DIR}/${fix}" ]; then
        MISSING_FIXTURES=1
        break
    fi
done

if [ "$MISSING_FIXTURES" -eq 1 ]; then
    say "Generating missing fixtures with generate_fixtures.go..."
    (cd "${ROOT_DIR}" && go run tests/fixtures/generate_fixtures.go "${FIXTURES_DIR}")
fi
success "All 7 package fixtures are ready"

# Step 2: Configure install command
INSTALL_CMD="curl -fsSL https://raw.githubusercontent.com/rfdetoni/mutate/main/install.sh | sh -s -- --system"

if [ "$MODE" = "local" ]; then
    say "Setting up local fast release server / mount..."
    LOCAL_TMP="$(mktemp -d)"
    REL_DIR="${LOCAL_TMP}/latest/download"
    mkdir -p "${REL_DIR}"
    (cd "${ROOT_DIR}" && CGO_ENABLED=0 go build -ldflags="-s -w" -o "${LOCAL_TMP}/mutate" ./cmd/mutate)
    tar -czf "${REL_DIR}/mutate_linux_amd64.tar.gz" -C "${LOCAL_TMP}" mutate
    (cd "${REL_DIR}" && sha256sum mutate_linux_amd64.tar.gz > SHA256SUMS)
    INSTALL_CMD="MUTATE_RELEASE_BASE_URL=file:///local_release /workspace/install.sh --system"
fi

# Step 3: Define OS Matrix
declare -A OS_IMAGES
OS_IMAGES["alpine"]="alpine:latest"
OS_IMAGES["debian"]="debian:bookworm-slim"
OS_IMAGES["ubuntu"]="ubuntu:24.04"
OS_IMAGES["fedora"]="fedora:latest"
OS_IMAGES["arch"]="archlinux:latest"

OS_LIST=()
if [ "$TARGET_OS" = "all" ]; then
    OS_LIST=("alpine" "debian" "ubuntu" "fedora" "arch")
else
    if [[ -v OS_IMAGES["$TARGET_OS"] ]]; then
        OS_LIST=("$TARGET_OS")
    else
        error "Unknown OS target: ${TARGET_OS}. Options: all, alpine, debian, ubuntu, fedora, arch"
    fi
fi

declare -A RESULTS

say "Starting Multi-OS Test Matrix (OS count: ${#OS_LIST[@]}, Mode: ${MODE})..."
echo ""

for os_name in "${OS_LIST[@]}"; do
    img="${OS_IMAGES[$os_name]}"
    printf "${BOLD}======================================================${NC}\n"
    say "Running test environment: ${BOLD}${os_name}${NC} (${img})"
    printf "${BOLD}======================================================${NC}\n"

    EXTRA_VOLUMES=()
    if [ "$MODE" = "local" ]; then
        EXTRA_VOLUMES+=("-v" "${LOCAL_TMP}:/local_release:ro")
    fi

    if ${RUNTIME} run --rm \
        -v "${ROOT_DIR}:/workspace:ro" \
        -v "${FIXTURES_DIR}:/fixtures:ro" \
        "${EXTRA_VOLUMES[@]}" \
        -e "MUTATE_SOURCE_URL=https://codeload.github.com/rfdetoni/mutate/tar.gz/refs/heads/main" \
        "${img}" \
        /workspace/tests/os_matrix/test_in_container.sh /fixtures "${INSTALL_CMD}"; then
        RESULTS["$os_name"]="PASS"
        success "Environment ${os_name} passed all tests!"
    else
        RESULTS["$os_name"]="FAIL"
        warn "Environment ${os_name} reported failures."
    fi
    echo ""
done

# Step 4: Print summary matrix
printf "${BOLD}======================================================${NC}\n"
printf "${BOLD}               M.U.T.A.T.E. TEST MATRIX SUMMARY       ${NC}\n"
printf "${BOLD}======================================================${NC}\n"
ALL_PASSED=1
for os_name in "${OS_LIST[@]}"; do
    res="${RESULTS[$os_name]}"
    if [ "$res" = "PASS" ]; then
        printf "  • %-15s [ ${GREEN}${BOLD}PASSED${NC} ]\n" "${os_name}"
    else
        printf "  • %-15s [ ${RED}${BOLD}FAILED${NC} ]\n" "${os_name}"
        ALL_PASSED=0
    fi
done
printf "${BOLD}======================================================${NC}\n"

if [ "$ALL_PASSED" -eq 1 ]; then
    success "All target OS environments passed all package tests!"
    exit 0
else
    error "Some environments failed."
fi
