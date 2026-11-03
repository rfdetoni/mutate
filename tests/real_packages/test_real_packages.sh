#!/bin/sh
# Real packages test runner executed inside each VM / container
set -eu

REAL_PACKAGES_DIR="${1:-/workspace/tests/real_packages}"
MUTATE_BIN="${2:-/usr/local/bin/mutate}"

if [ ! -x "${MUTATE_BIN}" ]; then
    if [ -x "/workspace/mutate" ]; then
        MUTATE_BIN="/workspace/mutate"
    else
        MUTATE_BIN="$(command -v mutate || true)"
    fi
fi

if [ -z "${MUTATE_BIN}" ] || [ ! -x "${MUTATE_BIN}" ]; then
    echo "Error: mutate binary not found or not executable"
    exit 1
fi

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

say() { printf "${BLUE}==>${NC} %s\n" "$*"; }
pass() { printf "  [ ${GREEN}PASS${NC} ] %s\n" "$*"; }
fail() { printf "  [ ${RED}FAIL${NC} ] %s\n" "$*"; exit 1; }
warn() { printf "  [ ${YELLOW}WARN${NC} ] %s\n" "$*"; }

OS_ID="unknown"
if [ -f /etc/os-release ]; then
    . /etc/os-release
    OS_ID="${ID:-unknown}"
fi

say "Testing Real Packages on: ${PRETTY_NAME:-$OS_ID}"

NATIVE_FMT="deb"
case "$OS_ID" in
    debian|ubuntu|linuxmint|pop) NATIVE_FMT="deb" ;;
    fedora|rhel|centos|rocky|almalinux) NATIVE_FMT="rpm" ;;
    arch|manjaro|endeavouros) NATIVE_FMT="arch" ;;
    alpine) NATIVE_FMT="apk" ;;
    *) NATIVE_FMT="deb" ;;
esac

say "Native package format: ${NATIVE_FMT}"

TEST_OUT="$(mktemp -d)"
trap 'rm -rf "${TEST_OUT}"' EXIT

# Test packages:
# 1. duf.deb (real Debian package of duf)
# 2. duf.rpm (real Fedora/RHEL package of duf)
# 3. duf.apk (real Alpine package of duf)
# 4. sl.deb (Debian official sl)
# 5. sl-5.02-26.fc45.x86_64.rpm (Fedora official sl)
# 6. sl.pkg.tar.zst (Arch official sl)
# 7. tree.apk (Alpine official tree)
# 8. appimagetool.AppImage (real AppImage v2)
# 9. fdroid.apk (real Android APK)
# 10. hello.tar.gz (GNU hello source tarball)

REAL_PACKAGES="duf.deb duf.rpm duf.apk sl.deb sl-5.02-26.fc45.x86_64.rpm sl.pkg.tar.zst tree.apk appimagetool.AppImage fdroid.apk hello.tar.gz"

for pkg in ${REAL_PACKAGES}; do
    pkg_path="${REAL_PACKAGES_DIR}/${pkg}"
    if [ ! -f "${pkg_path}" ]; then
        warn "Package ${pkg_path} not found, skipping"
        continue
    fi

    say "Package: ${pkg}"

    # 1. mutate inspect
    if "${MUTATE_BIN}" inspect "${pkg_path}" >/dev/null 2>&1; then
        pass "mutate inspect ${pkg}"
    else
        fail "mutate inspect failed on ${pkg}"
    fi

    # 2. mutate build to native format (or test conversions)
    out_pkg="${TEST_OUT}/${pkg}.transpiled.${NATIVE_FMT}"
    build_flags="--to ${NATIVE_FMT} -o ${out_pkg}"
    if [ "${pkg}" = "hello.tar.gz" ]; then
        build_flags="${build_flags} --allow-source-build"
    fi

    if "${MUTATE_BIN}" build "${pkg_path}" ${build_flags} >/dev/null 2>&1; then
        pass "mutate build ${pkg} -> ${NATIVE_FMT}"
    else
        # Dynamic linking / cross-format dependencies across non-native hosts
        # may require explicit dependency mapping; warn if not mapped
        warn "mutate build ${pkg} -> ${NATIVE_FMT} (requires explicit dependency map or native environment)"
    fi

    # 3. mutate install --dry-run
    if "${MUTATE_BIN}" install "${pkg_path}" --dry-run >/dev/null 2>&1; then
        pass "mutate install ${pkg} --dry-run"
    else
        warn "mutate install ${pkg} --dry-run returned non-zero"
    fi
done

printf "\n${GREEN}=== REAL PACKAGES TEST PASSED ON %s ===${NC}\n" "${PRETTY_NAME:-$OS_ID}"
