#!/bin/sh
# M.U.T.A.T.E. portable Linux installer
# curl -fsSL https://raw.githubusercontent.com/rfdetoni/mutate/main/install.sh | sh
set -eu
umask 077

REPO="rfdetoni/mutate"
METHOD=${MUTATE_INSTALL_METHOD:-auto}
VERSION=${MUTATE_VERSION:-latest}
REF=${MUTATE_REF:-main}
DEST=${MUTATE_INSTALL_DIR:-}
REGISTER_DESKTOP=${MUTATE_INSTALL_DESKTOP:-1}
DESKTOP_SCOPE=auto
RELEASES=${MUTATE_RELEASE_BASE_URL:-https://github.com/$REPO/releases}
SOURCE_URL=${MUTATE_SOURCE_URL:-https://codeload.github.com/$REPO/tar.gz/refs/heads/$REF}

say() { printf '%s\n' "==> $*" >&2; }
warn() { printf '%s\n' "WARN: $*" >&2; }
die() { printf '%s\n' "ERROR: $*" >&2; exit 1; }

usage() {
    cat <<'EOF'
M.U.T.A.T.E. installer (Linux, POSIX sh)
Usage: sh install.sh [--user | --system | --dir PATH] [--source | --release] [--version TAG] [--no-desktop]
  --user          Install to ~/.local/bin and register for the current user
  --system        Install to /usr/local/bin and register for all users
  --dir PATH      Install to a custom directory
  --source        Build from official source archive; bootstrap Go if necessary
  --release       Require a checksum-verified prebuilt GitHub release
  --version TAG   Select a release tag (default: latest)
  --desktop       Explicitly register the GUI launcher (already enabled by default)
  --no-desktop    Do not register a Linux application-menu launcher
  --help          Show help
Environment: MUTATE_INSTALL_DIR, MUTATE_INSTALL_METHOD, MUTATE_VERSION, MUTATE_REF, MUTATE_INSTALL_DESKTOP (1 by default; 0 disables)
EOF
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        --user) [ -n "${HOME:-}" ] || die "HOME is not set"; DEST="$HOME/.local/bin"; DESKTOP_SCOPE=user ;;
        --system) DEST="/usr/local/bin"; DESKTOP_SCOPE=system ;;
        --dir) [ "$#" -ge 2 ] || die "--dir needs a path"; DEST=$2; DESKTOP_SCOPE=auto; shift ;;
        --source) METHOD=source ;;
        --desktop) REGISTER_DESKTOP=1 ;;
        --no-desktop) REGISTER_DESKTOP=0 ;;
        --release) METHOD=release ;;
        --version) [ "$#" -ge 2 ] || die "--version needs a tag"; VERSION=$2; shift ;;
        -h|--help) usage; exit 0 ;;
        *) die "unknown argument: $1 (see --help)" ;;
    esac
    shift
done

[ "$(uname -s)" = "Linux" ] || die "this installer supports Linux only"
case "$METHOD" in auto|release|source) ;; *) die "invalid method: $METHOD" ;; esac
case "$REGISTER_DESKTOP" in 0|1) ;; *) die "MUTATE_INSTALL_DESKTOP must be 0 or 1" ;; esac
case "$VERSION" in
    ""|*[!a-zA-Z0-9._-]*) die "invalid release version: $VERSION" ;;
esac
case "$REF" in
    ""|/*|*..*|*[!a-zA-Z0-9._/-]*) die "invalid source ref: $REF" ;;
esac

case "$(uname -m)" in
    x86_64|amd64) ARCH=amd64; GO_ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64; GO_ARCH=arm64 ;;
    i386|i486|i586|i686) ARCH=386; GO_ARCH=386 ;;
    armv6l|armv7l|armv8l) ARCH=armv6; GO_ARCH=armv6l ;;
    riscv64) ARCH=riscv64; GO_ARCH=riscv64 ;;
    ppc64le) ARCH=ppc64le; GO_ARCH=ppc64le ;;
    s390x) ARCH=s390x; GO_ARCH=s390x ;;
    loongarch64|loong64) ARCH=loong64; GO_ARCH=loong64 ;;
    *) ARCH=unknown; GO_ARCH=unknown; warn "architecture $(uname -m): prebuilt release unavailable; source build requires installed Go" ;;
esac

if command -v curl >/dev/null 2>&1; then
    fetch() { curl --fail --location --silent --show-error --retry 2 --connect-timeout 15 --output "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
    fetch() { wget -q -O "$2" "$1"; }
else
    die "curl or wget is required"
fi
command -v tar >/dev/null 2>&1 || die "tar is required"
command -v mktemp >/dev/null 2>&1 || die "mktemp is required"

WORK=$(mktemp -d "${TMPDIR:-/tmp}/mutate-install.XXXXXX") || die "cannot create private workspace"
trap 'rm -rf "$WORK"' 0
trap 'exit 1' 1 2 3 15

sha256() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" | awk '{print $1}'
    elif command -v openssl >/dev/null 2>&1; then
        openssl dgst -sha256 "$1" | awk '{print $NF}'
    else
        die "SHA-256 verifier is required (sha256sum, shasum or openssl)"
    fi
}

verify() {
    expected=$2
    case "$expected" in *[!0-9a-f]*|"") die "invalid SHA-256 checksum" ;; esac
    [ "${#expected}" -eq 64 ] || die "invalid SHA-256 length"
    actual=$(sha256 "$1")
    [ "$actual" = "$expected" ] || die "SHA-256 mismatch: $(basename "$1")"
}

release_checksum() {
    awk -v asset="$2" '$2 == asset || $2 == "*" asset {print $1; exit}' "$1"
}

download_release() {
    [ "$ARCH" != unknown ] || return 1
    asset="mutate_linux_$ARCH.tar.gz"
    if [ "$VERSION" = latest ]; then
        base="$RELEASES/latest/download"
    else
        base="$RELEASES/download/$VERSION"
    fi
    say "Checking prebuilt release for linux/$ARCH..."
    if ! fetch "$base/$asset" "$WORK/$asset" 2>/dev/null; then
        return 1
    fi
    fetch "$base/SHA256SUMS" "$WORK/SHA256SUMS" || die "release checksum manifest unavailable; refusing unverified binary"
    hash=$(release_checksum "$WORK/SHA256SUMS" "$asset")
    [ -n "$hash" ] || die "no checksum for $asset in release manifest"
    verify "$WORK/$asset" "$hash"

    members=$(tar -tzf "$WORK/$asset") || die "invalid release archive"
    [ "$members" = mutate ] || die "unexpected release archive entries"
    mkdir -p "$WORK/prebuilt"
    tar -xzf "$WORK/$asset" -C "$WORK/prebuilt" || die "cannot unpack release archive"
    [ -f "$WORK/prebuilt/mutate" ] && [ ! -L "$WORK/prebuilt/mutate" ] || die "release binary is not a regular file"
    SOURCE_BINARY="$WORK/prebuilt/mutate"
    return 0
}

# Probe an explicitly local Go toolchain outside the caller's module.
# An unqualified 'go version' inside a project may transparently switch
# toolchains via GOTOOLCHAIN=auto, even when PATH points to an older Go.
local_go_version() {
    (cd "$WORK" && unset GOROOT GOTOOLDIR && GOTOOLCHAIN=local "$1" version) 2>/dev/null |
        awk '$1 == "go" && $2 == "version" {sub(/^go/, "", $3); print $3; exit}'
}

version_at_least() {
    awk -v got="$1" -v need="$2" 'BEGIN {
        split(got, g, "."); split(need, n, ".")
        for (i=1; i<=3; i++) {
            if (g[i]+0 > n[i]+0) exit 0
            if (g[i]+0 < n[i]+0) exit 1
        }
        exit 0
    }'
}

official_go_checksum() {
    # go.dev publishes one filename and sha256 per JSON object. Parsing the
    # adjacent fields avoids requiring jq, Python, or a distro package manager.
    awk -v file="$2" '
        index($0, "\"filename\": \"" file "\"") { found=1; next }
        found && /"sha256":/ {
            split($0, parts, "\""); print parts[4]; exit
        }
        found && /}/ { exit }
    ' "$1"
}

bootstrap_go() {
    [ "$GO_ARCH" != unknown ] || die "install Go >= $NEEDED_GO for $(uname -m) to build from source"
    command -v awk >/dev/null 2>&1 || die "awk is required for verified Go bootstrap"
    command -v gzip >/dev/null 2>&1 || die "gzip is required for Go bootstrap"
    go_archive="go$NEEDED_GO.linux-$GO_ARCH.tar.gz"
    say "Go >= $NEEDED_GO not found; obtaining temporary official Go toolchain ($GO_ARCH)..."
    fetch "https://go.dev/dl/?mode=json&include=all" "$WORK/go-releases.json" || die "cannot retrieve official Go checksums"
    checksum=$(official_go_checksum "$WORK/go-releases.json" "$go_archive")
    [ -n "$checksum" ] || die "official Go checksum not found for $go_archive"
    fetch "https://go.dev/dl/$go_archive" "$WORK/$go_archive" || die "cannot download official Go toolchain"
    verify "$WORK/$go_archive" "$checksum"
    mkdir -p "$WORK/toolchain"
    tar -xzf "$WORK/$go_archive" -C "$WORK/toolchain" || die "cannot unpack Go toolchain"
    GO_BIN="$WORK/toolchain/go/bin/go"
    [ -x "$GO_BIN" ] || die "downloaded Go toolchain is not executable"
}

build_from_source() {
    if [ "$VERSION" != latest ] && [ "$METHOD" != source ]; then
        die "release $VERSION is unavailable; will not silently substitute main branch"
    fi
    say "Downloading M.U.T.A.T.E. source ($REF)..."
    fetch "$SOURCE_URL" "$WORK/source.tar.gz" || die "cannot download source archive"
    mkdir -p "$WORK/source"
    tar -xzf "$WORK/source.tar.gz" -C "$WORK/source" || die "cannot unpack source archive"
    set -- "$WORK/source"/*
    [ "$#" -eq 1 ] && [ -f "$1/go.mod" ] || die "source archive has unexpected structure"
    SRC=$1
    NEEDED_GO=$(awk '$1 == "go" {print $2; exit}' "$SRC/go.mod")
    case "$NEEDED_GO" in ""|*[!0-9.]*) die "invalid go directive in source go.mod" ;; esac
    GO_BIN=
    if go_candidate=$(command -v go 2>/dev/null); then
        installed=$(local_go_version "$go_candidate")
        if version_at_least "$installed" "$NEEDED_GO"; then
            GO_BIN=$go_candidate
        elif [ -n "$installed" ]; then
            warn "local Go $installed is older than the required $NEEDED_GO; using a temporary verified toolchain"
        fi
    fi
    [ -n "$GO_BIN" ] || bootstrap_go
    actual=$(local_go_version "$GO_BIN")
    version_at_least "$actual" "$NEEDED_GO" ||
        die "selected Go toolchain ${actual:-unknown} is older than required $NEEDED_GO"
    say "Building with Go $actual..."
    (cd "$SRC" && unset GOROOT GOTOOLDIR &&
        CGO_ENABLED=0 GOOS=linux GOTOOLCHAIN=local "$GO_BIN" build -trimpath -ldflags="-s -w" -o "$WORK/mutate" ./cmd/mutate) ||
        die "Go build failed with toolchain $actual"
    [ -s "$WORK/mutate" ] || die "build produced no binary"
    SOURCE_BINARY="$WORK/mutate"
}

case "$METHOD" in
    source) build_from_source ;;
    release) download_release || die "no release binary for linux/$ARCH ($VERSION)" ;;
    auto)
        if ! download_release; then
            warn "no prebuilt release found; compiling from source"
            build_from_source
        fi
        ;;
esac

if [ -z "$DEST" ]; then
    if [ "$(id -u)" -eq 0 ] || { [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; }; then
        DEST=/usr/local/bin
    else
        [ -n "${HOME:-}" ] || die "HOME is not set; use --dir PATH"
        DEST="$HOME/.local/bin"
    fi
fi
case "$DEST" in /*) ;; *) die "install directory must be an absolute path: $DEST" ;; esac

PRIV=
if [ ! -d "$DEST" ]; then
    if mkdir -p "$DEST" 2>/dev/null; then :; else
        if command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
            PRIV=sudo
            sudo mkdir -p "$DEST" || die "cannot create $DEST"
        else
            die "cannot create $DEST; use --user or run with appropriate permissions"
        fi
    fi
fi
[ -d "$DEST" ] || die "not a directory: $DEST"
if [ ! -w "$DEST" ]; then
    if command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
        PRIV=sudo
    else
        die "cannot write to $DEST; use --user, --dir, or configure sudo"
    fi
fi

# Install via a temporary file in the destination filesystem, then rename.
# The final executable is never partially written.
new="$DEST/.mutate-new-$$"
trap 'if [ -n "$PRIV" ]; then $PRIV rm -f "$new"; else rm -f "$new"; fi; rm -rf "$WORK"' 0
if [ -n "$PRIV" ]; then
    sudo cp "$SOURCE_BINARY" "$new" && sudo chmod 0755 "$new" && sudo mv -f "$new" "$DEST/mutate" ||
        die "failed to install mutate in $DEST"
else
    cp "$SOURCE_BINARY" "$new" && chmod 0755 "$new" && mv -f "$new" "$DEST/mutate" ||
        die "failed to install mutate in $DEST"
fi

alias_path="$DEST/mut"
if [ -e "$alias_path" ] && [ ! -L "$alias_path" ]; then
    warn "$alias_path already exists and is not a symlink; left unchanged"
else
    if [ -n "$PRIV" ]; then
        sudo rm -f "$alias_path" && sudo ln -s mutate "$alias_path" || die "cannot create mut alias"
    else
        rm -f "$alias_path" && ln -s mutate "$alias_path" || die "cannot create mut alias"
    fi
fi

say "Installed: $DEST/mutate"
say "Alias:     $DEST/mut (unless previously occupied)"
case ":$PATH:" in
    *":$DEST:"*) ;;
    *) warn "$DEST is not in PATH. Add: export PATH=\"$DEST:\$PATH\"" ;;
esac
say "Done. Run: $DEST/mutate --version"

# Register a menu entry for every successful installation by default.
# User installs write into XDG_DATA_HOME; system installs use /usr/local/share.
# Registration never executes the installed binary or fetches any assets.
register_desktop() {
    binary="$DEST/mutate"
    # Desktop Entry Exec quoting protects spaces. Reject characters requiring
    # additional escaping so custom paths cannot inject desktop commands.
    case "$binary" in
        *[!a-zA-Z0-9_./+@:\ -]*)
            warn "cannot safely represent $binary in a desktop launcher"
            return 1 ;;
    esac

    scope=$DESKTOP_SCOPE
    if [ "$scope" = auto ]; then
        case "$DEST" in
            /usr/local/bin|/usr/bin) scope=system ;;
            *)
                if [ -n "${HOME:-}" ]; then
                    case "$DEST" in
                        "$HOME"/*) scope=user ;;
                    esac
                fi
                if [ "$scope" = auto ]; then
                    if [ "$(id -u)" -eq 0 ]; then scope=system; else scope=user; fi
                fi ;;
        esac
    fi

    if [ "$scope" = system ]; then
        applications=/usr/local/share/applications
    else
        [ -n "${HOME:-}" ] || { warn "HOME is not set; cannot register a user launcher"; return 1; }
        data_home=${XDG_DATA_HOME:-"$HOME/.local/share"}
        case "$data_home" in
            /*) ;;
            *) data_home="$HOME/.local/share" ;;
        esac
        applications="$data_home/applications"
    fi

    cat > "$WORK/mutate.desktop" <<EOF
[Desktop Entry]
Version=1.0
Type=Application
Name=M.U.T.A.T.E.
Comment=Conversor e instalador de pacotes Linux
Exec="$binary" gui
Icon=package-x-generic
Terminal=false
Categories=Utility;System;
StartupNotify=true
EOF

    temp_entry="$applications/.mutate-$$.desktop"
    desktop_priv=
    if [ "$scope" = system ] && [ "$(id -u)" -ne 0 ] &&
       { [ ! -d "$applications" ] || [ ! -w "$applications" ]; }; then
        if command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
            desktop_priv=sudo
        else
            warn "system application menu is not writable; install with administrator permissions"
            return 1
        fi
    fi
    if [ -n "$desktop_priv" ]; then
        "$desktop_priv" mkdir -p "$applications" || return 1
        "$desktop_priv" cp "$WORK/mutate.desktop" "$temp_entry" || return 1
        "$desktop_priv" chmod 0644 "$temp_entry" || return 1
        "$desktop_priv" mv -f "$temp_entry" "$applications/mutate.desktop" || return 1
    else
        mkdir -p "$applications" || return 1
        cp "$WORK/mutate.desktop" "$temp_entry" || return 1
        chmod 0644 "$temp_entry" || return 1
        mv -f "$temp_entry" "$applications/mutate.desktop" || return 1
    fi
    say "Application menu: $applications/mutate.desktop"
}

if [ "$REGISTER_DESKTOP" = 1 ]; then
    if ! register_desktop; then
        warn "binary installed, but the application-menu shortcut could not be registered"
    fi
fi
