#!/bin/sh
# M.U.T.A.T.E. - Universal Linux Transpiler Installer
# https://github.com/rfdetoni/mutate
set -eu

BIN_NAME="mutate"
ALIAS_NAME="mut"

# Detect target directory
if [ -w "/usr/local/bin" ]; then
    INSTALL_DIR="/usr/local/bin"
elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
    INSTALL_DIR="/usr/local/bin"
    SUDO="sudo"
else
    INSTALL_DIR="${HOME}/.local/bin"
    mkdir -p "${INSTALL_DIR}"
    SUDO=""
fi

echo "==> Building ${BIN_NAME} (pure Go, CGO_ENABLED=0)..."
if ! command -v go >/dev/null 2>&1; then
    echo "Error: Go compiler is required to build from source." >&2
    exit 1
fi

TMP_BUILD="$(mktemp -d)"
trap 'rm -rf "${TMP_BUILD}"' EXIT

CGO_ENABLED=0 go build -ldflags="-s -w" -o "${TMP_BUILD}/${BIN_NAME}" ./cmd/mutate

echo "==> Installing to ${INSTALL_DIR}..."
if [ "${INSTALL_DIR}" = "/usr/local/bin" ] && [ ! -w "/usr/local/bin" ]; then
    sudo install -m 0755 "${TMP_BUILD}/${BIN_NAME}" "${INSTALL_DIR}/${BIN_NAME}"
    sudo ln -sf "${INSTALL_DIR}/${BIN_NAME}" "${INSTALL_DIR}/${ALIAS_NAME}"
else
    install -m 0755 "${TMP_BUILD}/${BIN_NAME}" "${INSTALL_DIR}/${BIN_NAME}"
    ln -sf "${INSTALL_DIR}/${BIN_NAME}" "${INSTALL_DIR}/${ALIAS_NAME}"
fi

echo "==> Installation complete!"
echo "    Binary: ${INSTALL_DIR}/${BIN_NAME}"
echo "    Alias:  ${INSTALL_DIR}/${ALIAS_NAME}"
echo ""
"${INSTALL_DIR}/${BIN_NAME}" --version || true
