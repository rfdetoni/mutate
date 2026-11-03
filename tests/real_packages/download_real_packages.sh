#!/bin/sh
# Download real upstream packages for testing
set -eu

DIR="$(cd "$(dirname "$0")" && pwd)"
mkdir -p "${DIR}"

say() { printf "\033[0;34m==>\033[0m %s\n" "$*"; }

# 1. duf (deb, rpm, apk)
if [ ! -f "${DIR}/duf.deb" ]; then
    say "Downloading duf.deb..."
    curl -fsSL -o "${DIR}/duf.deb" "https://github.com/muesli/duf/releases/download/v0.9.0/duf_0.9.0_linux_amd64.deb"
fi

if [ ! -f "${DIR}/duf.rpm" ]; then
    say "Downloading duf.rpm..."
    curl -fsSL -o "${DIR}/duf.rpm" "https://github.com/muesli/duf/releases/download/v0.9.0/duf_0.9.0_linux_amd64.rpm"
fi

if [ ! -f "${DIR}/duf.apk" ]; then
    say "Downloading duf.apk..."
    curl -fsSL -o "${DIR}/duf.apk" "https://github.com/muesli/duf/releases/download/v0.9.0/duf_0.9.0_linux_amd64.apk"
fi

# 2. sl (deb, rpm, arch)
if [ ! -f "${DIR}/sl.deb" ]; then
    say "Downloading sl.deb..."
    curl -fsSL -o "${DIR}/sl.deb" "http://ftp.debian.org/debian/pool/main/s/sl/sl_5.02-1_amd64.deb"
fi

if [ ! -f "${DIR}/sl.pkg.tar.zst" ]; then
    say "Downloading sl.pkg.tar.zst..."
    curl -fsSL -o "${DIR}/sl.pkg.tar.zst" "https://archlinux.org/packages/extra/x86_64/sl/download/"
fi

# 3. Alpine tree.apk
if [ ! -f "${DIR}/tree.apk" ]; then
    say "Downloading tree.apk..."
    curl -fsSL -o "${DIR}/tree.apk" "https://dl-cdn.alpinelinux.org/alpine/v3.20/main/x86_64/tree-2.1.1-r0.apk"
fi

# 4. AppImage (appimagetool)
if [ ! -f "${DIR}/appimagetool.AppImage" ]; then
    say "Downloading appimagetool.AppImage..."
    curl -fsSL -o "${DIR}/appimagetool.AppImage" "https://github.com/AppImage/AppImageKit/releases/download/12/appimagetool-x86_64.AppImage"
fi

# 5. Android APK (F-Droid)
if [ ! -f "${DIR}/fdroid.apk" ]; then
    say "Downloading fdroid.apk..."
    curl -fsSL -o "${DIR}/fdroid.apk" "https://f-droid.org/F-Droid.apk"
fi

# 6. Source tarball (hello)
if [ ! -f "${DIR}/hello.tar.gz" ]; then
    say "Downloading hello.tar.gz..."
    curl -fsSL -o "${DIR}/hello.tar.gz" "https://ftp.gnu.org/gnu/hello/hello-2.12.1.tar.gz"
fi

say "All real test packages downloaded to ${DIR}"
