#!/usr/bin/env bash
set -euo pipefail

NO_SYMLINK=0
for arg in "$@"; do
    if [ "$arg" == "--no-symlink" ]; then
        NO_SYMLINK=1
    fi
done

echo "[+] Building gh-install..."
make build

echo "[+] Registering extension with GitHub CLI..."
EXTENSION_DIR="${HOME}/.local/share/gh/extensions/gh-install"

if [ -L "${EXTENSION_DIR}" ]; then
    echo "[*] Symlink already exists at ${EXTENSION_DIR}."
else
    gh extension install .
fi

echo "[+] Managing global binary in /usr/local/bin/gh-install..."
if [ "$NO_SYMLINK" -eq 1 ]; then
    echo "[*] --no-symlink specified. Hard copying binary..."
    sudo rm -f /usr/local/bin/gh-install
    sudo cp "$(pwd)/gh-install" /usr/local/bin/gh-install
else
    echo "[*] Symlinking binary..."
    sudo rm -f /usr/local/bin/gh-install
    sudo ln -s "$(pwd)/gh-install" /usr/local/bin/gh-install
fi

echo "[+] Done. Test with: gh install --help or gh-install --help"
