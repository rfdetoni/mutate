#!/bin/sh
# Offline smoke tests for install.sh. No network access, real Go build, or sudo.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' 0
die() { echo "FAIL: $*" >&2; exit 1; }
mkdir -p "$TMP/fixtures/source/mutate-main/cmd/mutate" "$TMP/fixtures/release" "$TMP/fixtures/bin" "$TMP/home"
printf 'module github.com/rfdetoni/mutate\ngo 1.26.4\n' > "$TMP/fixtures/source/mutate-main/go.mod"
printf 'package main\n' > "$TMP/fixtures/source/mutate-main/cmd/mutate/main.go"
tar -czf "$TMP/fixtures/source.tar.gz" -C "$TMP/fixtures/source" mutate-main
printf '#!/bin/sh\necho release-binary\n' > "$TMP/fixtures/release/mutate"
chmod 755 "$TMP/fixtures/release/mutate"
tar -czf "$TMP/fixtures/release.tar.gz" -C "$TMP/fixtures/release" mutate
printf '%s  mutate_linux_amd64.tar.gz\n' "$(sha256sum "$TMP/fixtures/release.tar.gz" | awk '{print $1}')" > "$TMP/fixtures/SHA256SUMS"

mkdir -p "$TMP/fixtures/toolchain/go/bin"
cat > "$TMP/fixtures/toolchain/go/bin/go" <<'EOF'
#!/bin/sh
case "$1" in
  version) echo "go version go1.26.4 linux/amd64" ;;
  build)
    out=
    while [ "$#" -gt 0 ]; do
      if [ "$1" = "-o" ]; then out=$2; shift 2; else shift; fi
    done
    printf '#!/bin/sh\necho source-binary\n' > "$out"
    chmod 755 "$out" ;;
  *) exit 1 ;;
esac
EOF
chmod 755 "$TMP/fixtures/toolchain/go/bin/go"
tar -czf "$TMP/fixtures/go.tar.gz" -C "$TMP/fixtures/toolchain" go
GOHASH=$(sha256sum "$TMP/fixtures/go.tar.gz" | awk '{print $1}')
printf '{\n  "filename": "go1.26.4.linux-amd64.tar.gz",\n  "sha256": "%s"\n}\n' "$GOHASH" > "$TMP/fixtures/go.json"

cat > "$TMP/fixtures/bin/curl" <<'EOF'
#!/bin/sh
dest=
url=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output|-o) dest=$2; shift 2 ;;
    http*) url=$1; shift ;;
    *) shift ;;
  esac
done
[ -n "$dest" ] && [ -n "$url" ] || exit 98
case "$url" in
  *mutate_linux_amd64.tar.gz)
    [ "$MOCK_RELEASE_MISSING" = 0 ] || exit 22
    cp "$MOCK_FIXTURES/release.tar.gz" "$dest" ;;
  *SHA256SUMS)
    [ "$MOCK_RELEASE_MISSING" = 0 ] || exit 22
    cp "$MOCK_FIXTURES/SHA256SUMS" "$dest" ;;
  *source.test*|*codeload*) cp "$MOCK_FIXTURES/source.tar.gz" "$dest" ;;
  *mode=json*) cp "$MOCK_FIXTURES/go.json" "$dest" ;;
  *go1.26.4.linux-amd64.tar.gz) cp "$MOCK_FIXTURES/go.tar.gz" "$dest" ;;
  *) echo "unexpected URL $url" >&2; exit 99 ;;
esac
EOF
chmod 755 "$TMP/fixtures/bin/curl"
cat > "$TMP/fixtures/bin/go" <<'EOF'
#!/bin/sh
case "$1" in
 version)
   # Simulate an old installed binary that reports the auto-selected
   # go1.26.4 from the caller's go.mod, but runs 1.24.0 with local.
   if [ "${MOCK_LOCAL_GO_OLD:-0}" = 1 ]; then
     if [ "${GOTOOLCHAIN:-auto}" = local ]; then
       echo "go version go1.24.0 linux/amd64"
     else
       echo "go version go1.26.4 linux/amd64"
     fi
   else
     echo "go version go1.26.4 linux/amd64"
   fi ;;
 mod) [ "$2" = tidy ] || exit 1 ;;
 build)
   if [ "${MOCK_LOCAL_GO_OLD:-0}" = 1 ]; then
     echo "go: go.mod requires go >= 1.26.4 (running go 1.24.0; GOTOOLCHAIN=local)" >&2
     exit 1
   fi
   printf '%s\n' "$*" >> "$MOCK_FIXTURES/go-build-args.log"
   out=
   while [ "$#" -gt 0 ]; do
     if [ "$1" = "-o" ]; then out=$2; shift 2; else shift; fi
   done
   printf '#!/bin/sh\necho source-binary\n' > "$out"
   chmod 755 "$out" ;;
 *) exit 1;;
esac
EOF
chmod 755 "$TMP/fixtures/bin/go"
cp "$TMP/fixtures/bin/go" "$TMP/fixtures/go-saved"

export MOCK_FIXTURES="$TMP/fixtures"
export MOCK_RELEASE_MISSING=0
export MUTATE_RELEASE_BASE_URL="https://release.test"
export MUTATE_SOURCE_URL="https://source.test"
export HOME="$TMP/home"
export XDG_DATA_HOME="$HOME/.local/share"
unset MUTATE_INSTALL_DESKTOP
export PATH="$TMP/fixtures/bin:/usr/bin:/bin"
export MUTATE_INSTALL_DIR="$TMP/home/.local/bin"

case "$(uname -m)" in x86_64|amd64) ;; *) echo "SKIP: integration tests require amd64"; exit 0;; esac

sh "$ROOT/install.sh" --release > "$TMP/release.log" 2>&1 ||
  { cat "$TMP/release.log"; die "release install"; }
[ "$("$MUTATE_INSTALL_DIR/mutate")" = release-binary ] || die "release content"
grep -q 'Native Gio GUI is not installed' "$TMP/release.log" ||
  die "CLI-only installation must report missing native GUI"
[ -L "$MUTATE_INSTALL_DIR/mut" ] || die "missing mut symlink"
DESKTOP_ENTRY="$XDG_DATA_HOME/applications/mutate.desktop"
[ -f "$DESKTOP_ENTRY" ] || die "desktop shortcut not registered by default"
grep -Fq "Exec=\"$MUTATE_INSTALL_DIR/mutate\" gui" "$DESKTOP_ENTRY" ||
  die "desktop launcher does not reference installed binary"
grep -q '^Terminal=false$' "$DESKTOP_ENTRY" ||
  die "desktop launcher should not open a terminal"

MOCK_RELEASE_MISSING=1
export MOCK_RELEASE_MISSING
sh "$ROOT/install.sh" > "$TMP/source.log" 2>&1 ||
  { cat "$TMP/source.log"; die "source fallback"; }
[ "$("$MUTATE_INSTALL_DIR/mutate")" = source-binary ] || die "source content"
[ -f "$DESKTOP_ENTRY" ] || die "source fallback did not register desktop entry"

# Regression: 'go version' reports 1.26.4 because auto-select reads the
# caller's project, while the underlying local Go is actually 1.24.0.
# The installer must reject that local toolchain and bootstrap Go 1.26.4.
MOCK_LOCAL_GO_OLD=1
export MOCK_LOCAL_GO_OLD
sh "$ROOT/install.sh" --source > "$TMP/auto-toolchain.log" 2>&1 ||
  { cat "$TMP/auto-toolchain.log"; die "auto-selected toolchain mismatch"; }
grep -q 'local Go 1.24.0 is older than the required 1.26.4' "$TMP/auto-toolchain.log" ||
  die "did not detect the underlying old Go binary"
grep -q 'temporary official Go toolchain' "$TMP/auto-toolchain.log" ||
  die "did not bootstrap a verified compatible toolchain"
[ "$("$MUTATE_INSTALL_DIR/mutate")" = source-binary ] ||
  die "compatible Go build did not produce a working binary"
MOCK_LOCAL_GO_OLD=0
export MOCK_LOCAL_GO_OLD

# Explicit opt-out should not recreate a missing entry.
MOCK_RELEASE_MISSING=0
export MOCK_RELEASE_MISSING
rm -f "$DESKTOP_ENTRY"
sh "$ROOT/install.sh" --release --no-desktop > "$TMP/no-desktop.log" 2>&1 ||
  { cat "$TMP/no-desktop.log"; die "desktop opt-out"; }
[ ! -f "$DESKTOP_ENTRY" ] || die "desktop entry created despite --no-desktop"

# Explicit flag must override the environment setting.
MUTATE_INSTALL_DESKTOP=0 sh "$ROOT/install.sh" --release --desktop > "$TMP/desktop-override.log" 2>&1 ||
  { cat "$TMP/desktop-override.log"; die "desktop override"; }
[ -f "$DESKTOP_ENTRY" ] || die "--desktop did not override the opt-out"

MOCK_RELEASE_MISSING=0
export MOCK_RELEASE_MISSING
printf '%064d  mutate_linux_amd64.tar.gz\n' 0 > "$TMP/fixtures/SHA256SUMS"
rm -f "$MUTATE_INSTALL_DIR/mutate"
if sh "$ROOT/install.sh" > "$TMP/badhash.log" 2>&1; then die "accepted bad hash"; fi
grep -q 'SHA-256 mismatch' "$TMP/badhash.log" || die "missing mismatch error"
[ ! -e "$MUTATE_INSTALL_DIR/mutate" ] || die "installed corrupt release"

rm -f "$TMP/fixtures/bin/go"
MOCK_RELEASE_MISSING=1
export MOCK_RELEASE_MISSING
sh "$ROOT/install.sh" --source > "$TMP/bootstrap.log" 2>&1 ||
  { cat "$TMP/bootstrap.log"; die "Go bootstrap"; }
grep -q 'temporary official Go toolchain' "$TMP/bootstrap.log" || die "toolchain not bootstrapped"
[ "$("$MUTATE_INSTALL_DIR/mutate")" = source-binary ] || die "bootstrap content"

# Native Gio dependency management tests use mocked package managers.
# They must never touch the runner's real APT packages or require root.
cp "$TMP/fixtures/go-saved" "$TMP/fixtures/bin/go"
chmod 755 "$TMP/fixtures/bin/go"
mkdir -p "$TMP/fixtures/source/mutate-main/desktop"
printf 'module github.com/rfdetoni/mutate/desktop\ngo 1.26.4\n' > "$TMP/fixtures/source/mutate-main/desktop/go.mod"
printf 'package main\n' > "$TMP/fixtures/source/mutate-main/desktop/main.go"
tar -czf "$TMP/fixtures/source.tar.gz" -C "$TMP/fixtures/source" mutate-main

cat > "$TMP/fixtures/bin/pkg-config" <<'EOF'
#!/bin/sh
[ "$1" = --exists ] || exit 1
[ -f "$MOCK_FIXTURES/gui-installed" ] || exit 1
case "$*" in
  *"egl glesv2"*) : ;;
  *"x11 xcursor xkbcommon-x11"*) : ;;
  *"wayland-client"*) [ "${MOCK_GUI_NO_WAYLAND:-0}" = 0 ] ;;
  *) exit 1 ;;
esac
EOF
cat > "$TMP/fixtures/bin/cc" <<'EOF'
#!/bin/sh
# A present .pc file alone does not guarantee compilable Gio headers.
[ -f "$MOCK_FIXTURES/gui-installed" ] || exit 1
if [ "${MOCK_GUI_BROKEN_HEADERS:-0}" = 1 ]; then
  echo "fatal error: glib-object.h: No such file or directory" >&2
  exit 1
fi
EOF
cat > "$TMP/fixtures/bin/apt-cache" <<'EOF'
#!/bin/sh
[ "$1" = show ] || exit 1
case "$2" in
  libwebkitgtk-6.0-dev) [ "${MOCK_GUI_LEGACY:-0}" = 0 ] ;;
  libwebkit2gtk-4.1-dev) [ "${MOCK_GUI_LEGACY:-0}" = 1 ] ;;
  *) exit 1 ;;
esac
EOF
cat > "$TMP/fixtures/bin/apt-get" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >> "$MOCK_FIXTURES/gui-apt.log"
case "$1" in
  install) touch "$MOCK_FIXTURES/gui-installed" ;;
  update) : ;;
  *) exit 1 ;;
esac
EOF
cat > "$TMP/fixtures/bin/sudo" <<'EOF'
#!/bin/sh
if [ "$1" = "-n" ] && [ "$2" = true ]; then
  [ "${MOCK_GUI_SUDO_DENY:-0}" = 0 ]
  exit $?
fi
[ "${MOCK_GUI_SUDO_DENY:-0}" = 0 ] || exit 1
exec "$@"
EOF
chmod 755 "$TMP/fixtures/bin/pkg-config" "$TMP/fixtures/bin/apt-cache" \
  "$TMP/fixtures/bin/apt-get" "$TMP/fixtures/bin/sudo" "$TMP/fixtures/bin/cc"

MOCK_GUI_LEGACY=0
export MOCK_GUI_LEGACY
MOCK_RELEASE_MISSING=1
export MOCK_RELEASE_MISSING
rm -f "$TMP/fixtures/gui-installed" "$TMP/fixtures/gui-apt.log"
sh "$ROOT/install.sh" --source > "$TMP/gui-auto.log" 2>&1 ||
  { cat "$TMP/gui-auto.log"; die "automatic Gio dependencies"; }
[ "$("$MUTATE_INSTALL_DIR/mutate")" = source-binary ] || die "GUI install broke CLI"
[ -x "$MUTATE_INSTALL_DIR/mutate-gio" ] || die "native Gio shell not installed"
grep -q 'install -y --no-install-recommends build-essential pkg-config libwayland-dev' \
  "$TMP/fixtures/gui-apt.log" || die "Gio graphics development packages not installed"

# An already provisioned host must not invoke apt-get again.
rm -f "$TMP/fixtures/gui-apt.log"
sh "$ROOT/install.sh" --source > "$TMP/gui-present.log" 2>&1 ||
  { cat "$TMP/gui-present.log"; die "GUI dependencies already present"; }
[ ! -e "$TMP/fixtures/gui-apt.log" ] || die "reinstalled Gio dependencies unnecessarily"

# An X11-only machine must build Gio without installing the Wayland stack.
MOCK_GUI_NO_WAYLAND=1
export MOCK_GUI_NO_WAYLAND
rm -f "$TMP/fixtures/gui-apt.log"
sh "$ROOT/install.sh" --source > "$TMP/gui-x11.log" 2>&1 ||
  { cat "$TMP/gui-x11.log"; die "Gio X11-only build"; }
[ ! -e "$TMP/fixtures/gui-apt.log" ] || die "Gio reinstalled packages for X11-only host"
grep -q -- '-tags=nowayland' "$TMP/fixtures/go-build-args.log" ||
  die "Gio nowayland build tag missing"
MOCK_GUI_NO_WAYLAND=0
export MOCK_GUI_NO_WAYLAND

# Opt-outs must prevent privileged dependency installation.
rm -f "$MUTATE_INSTALL_DIR/mutate-gio" "$TMP/fixtures/gui-installed" "$TMP/fixtures/gui-apt.log"
MUTATE_INSTALL_GUI_DEPS=0 sh "$ROOT/install.sh" --source > "$TMP/gui-env-optout.log" 2>&1 ||
  { cat "$TMP/gui-env-optout.log"; die "GUI env opt-out"; }
[ ! -e "$TMP/fixtures/gui-apt.log" ] && [ ! -e "$MUTATE_INSTALL_DIR/mutate-gio" ] ||
  die "GUI dependencies installed despite environment opt-out"
sh "$ROOT/install.sh" --source --no-gui-deps > "$TMP/gui-flag-optout.log" 2>&1 ||
  { cat "$TMP/gui-flag-optout.log"; die "GUI flag opt-out"; }
[ ! -e "$TMP/fixtures/gui-apt.log" ] || die "--no-gui-deps was ignored"

# An explicit --gui-deps overrides the environment opt-out.
MUTATE_INSTALL_GUI_DEPS=0 sh "$ROOT/install.sh" --source --gui-deps > "$TMP/gui-override.log" 2>&1 ||
  { cat "$TMP/gui-override.log"; die "GUI dependency override"; }
[ -x "$MUTATE_INSTALL_DIR/mutate-gio" ] || die "--gui-deps did not build native GUI"

# A headless host without sudo must keep the CLI functional and warn.
MOCK_GUI_SUDO_DENY=1
export MOCK_GUI_SUDO_DENY
rm -f "$TMP/fixtures/gui-installed" "$TMP/fixtures/gui-apt.log" "$MUTATE_INSTALL_DIR/mutate-gio"
sh "$ROOT/install.sh" --source > "$TMP/gui-no-sudo.log" 2>&1 ||
  { cat "$TMP/gui-no-sudo.log"; die "GUI no-sudo graceful fallback"; }
[ "$("$MUTATE_INSTALL_DIR/mutate")" = source-binary ] || die "no-sudo failed CLI install"
[ ! -e "$TMP/fixtures/gui-apt.log" ] || die "apt called without privilege"
grep -q 'skipping GUI dependencies' "$TMP/gui-no-sudo.log" ||
  die "missing warning about GUI dependencies"
MOCK_GUI_SUDO_DENY=0
export MOCK_GUI_SUDO_DENY

printf '#!/bin/sh\necho previous\n' > "$MUTATE_INSTALL_DIR/mutate"
chmod 755 "$MUTATE_INSTALL_DIR/mutate"
MOCK_FIXTURES="$TMP/absent"
export MOCK_FIXTURES
if sh "$ROOT/install.sh" --source > "$TMP/failed.log" 2>&1; then die "unexpected success"; fi
[ "$("$MUTATE_INSTALL_DIR/mutate")" = previous ] || die "existing binary lost after failure"

echo "Installer integration tests passed"
