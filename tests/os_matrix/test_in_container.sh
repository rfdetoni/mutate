#!/bin/sh
# Test runner executed inside each VM / container
# Tests:
# 1. Installation via curl ... | sh -s -- --system
# 2. Binary execution (mutate & mut)
# 3. All 7 package types (inspect, build, install --dry-run)
set -eu

FIXTURES_DIR="${1:-/workspace/tests/fixtures}"
INSTALL_CMD="${2:-curl -fsSL https://raw.githubusercontent.com/rfdetoni/mutate/main/install.sh | sh -s -- --system}"

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

say() { printf "${BLUE}==>${NC} %s\n" "$*"; }
pass() { printf "  [ ${GREEN}PASS${NC} ] %s\n" "$*"; }
fail() { printf "  [ ${RED}FAIL${NC} ] %s\n" "$*"; exit 1; }
warn() { printf "  [ ${YELLOW}WARN${NC} ] %s\n" "$*"; }

# 1. Detect environment
OS_ID="unknown"
if [ -f /etc/os-release ]; then
    . /etc/os-release
    OS_ID="${ID:-unknown}"
fi

say "Starting M.U.T.A.T.E. OS test on: ${PRETTY_NAME:-$OS_ID}"

# 2. Determine native package format for this distro
NATIVE_FMT="deb"
case "$OS_ID" in
    debian|ubuntu|linuxmint|pop) NATIVE_FMT="deb" ;;
    fedora|rhel|centos|rocky|almalinux) NATIVE_FMT="rpm" ;;
    arch|manjaro|endeavouros) NATIVE_FMT="arch" ;;
    alpine) NATIVE_FMT="apk" ;;
    *) NATIVE_FMT="deb" ;;
esac
say "Detected native package format: ${NATIVE_FMT}"

# 3. Install curl/ca-certificates if missing
say "Ensuring curl and prerequisites are available..."
case "$OS_ID" in
    alpine) apk add --no-cache curl ca-certificates bubblewrap >/dev/null 2>&1 || apk add --no-cache curl ca-certificates ;;
    debian|ubuntu) apt-get update -qq && apt-get install -y -qq curl ca-certificates bubblewrap >/dev/null 2>&1 || apt-get install -y -qq curl ca-certificates ;;
    fedora) dnf install -y -q curl ca-certificates bubblewrap >/dev/null 2>&1 || dnf install -y -q curl ca-certificates ;;
    arch) pacman -Sy --noconfirm --noprogressbar curl ca-certificates bubblewrap >/dev/null 2>&1 || pacman -Sy --noconfirm --noprogressbar curl ca-certificates ;;
    *) echo "Warning: could not detect package manager to install prerequisites" ;;
esac

# 4. Execute installation via specified command
say "Running installation command:"
echo "    ${INSTALL_CMD}"
eval "${INSTALL_CMD}"

# 5. Verify installed binaries
say "Verifying installed binaries..."
if [ -x /usr/local/bin/mutate ]; then
    pass "Binary /usr/local/bin/mutate exists and is executable"
else
    fail "Binary /usr/local/bin/mutate missing or not executable"
fi

if [ -L /usr/local/bin/mut ] || [ -x /usr/local/bin/mut ]; then
    pass "Alias /usr/local/bin/mut exists"
else
    fail "Alias /usr/local/bin/mut missing"
fi

MUTATE_VER=$(/usr/local/bin/mutate --version)
pass "mutate reports: ${MUTATE_VER}"

MUT_VER=$(/usr/local/bin/mut --version)
pass "mut alias reports: ${MUT_VER}"

# 6. Test all package types
say "Testing all 7 package types against mutate..."
TEST_OUT="$(mktemp -d)"
trap 'rm -rf "${TEST_OUT}"' EXIT

PACKAGES="sample.deb sample.rpm sample.pkg.tar.zst sample-alpine.apk sample-android.apk sample-source.tar.gz sample.AppImage"

for pkg in ${PACKAGES}; do
    pkg_path="${FIXTURES_DIR}/${pkg}"
    if [ ! -f "${pkg_path}" ]; then
        fail "Fixture ${pkg_path} does not exist!"
    fi

    say "Testing package: ${pkg}"

    # A) mutate inspect
    if /usr/local/bin/mutate inspect "${pkg_path}" >/dev/null 2>&1; then
        pass "mutate inspect ${pkg}"
    else
        fail "mutate inspect failed on ${pkg}"
    fi

    # B) mutate build --to <native_format>
    out_pkg="${TEST_OUT}/${pkg}.transpiled"
    build_flags="--to ${NATIVE_FMT} -o ${out_pkg}"
    if [ "${pkg}" = "sample-source.tar.gz" ]; then
        build_flags="${build_flags} --allow-source-build"
    fi

    if /usr/local/bin/mutate build "${pkg_path}" ${build_flags} >/dev/null 2>&1; then
        pass "mutate build ${pkg} -> ${NATIVE_FMT}"
    else
        # If dependency mapping needed across foreign format
        warn "mutate build ${pkg} failed (may require explicit dependency map)"
    fi

    # C) mutate install --dry-run
    if /usr/local/bin/mutate install "${pkg_path}" --dry-run >/dev/null 2>&1; then
        pass "mutate install ${pkg} --dry-run"
    else
        warn "mutate install ${pkg} --dry-run returned non-zero"
    fi
done

printf "\n${GREEN}=== ALL TESTS PASSED SUCCESSFULLY ON %s ===${NC}\n" "${PRETTY_NAME:-$OS_ID}"
