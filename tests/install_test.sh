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
 version) echo "go version go1.26.4 linux/amd64" ;;
 build)
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

printf '#!/bin/sh\necho previous\n' > "$MUTATE_INSTALL_DIR/mutate"
chmod 755 "$MUTATE_INSTALL_DIR/mutate"
MOCK_FIXTURES="$TMP/absent"
export MOCK_FIXTURES
if sh "$ROOT/install.sh" --source > "$TMP/failed.log" 2>&1; then die "unexpected success"; fi
[ "$("$MUTATE_INSTALL_DIR/mutate")" = previous ] || die "existing binary lost after failure"

echo "Installer integration tests passed"
