#!/usr/bin/env bash
set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BOLD='\033[1m'
NC='\033[0m'

# Status icons
GREEN_DOT="${GREEN}${BOLD}🟢${NC}"
RED_DOT="${RED}${BOLD}🔴${NC}"
YELLOW_DOT="${YELLOW}${BOLD}🟡${NC}"

# Colors for text
GREEN_TXT="${GREEN}${BOLD}"
RED_TXT="${RED}${BOLD}"
YELLOW_TXT="${YELLOW}${BOLD}"

DIRECT=0
FORCE=0
GLOBAL=0

for arg in "$@"; do
    case "$arg" in
        --direct)    DIRECT=1 ;;
        --symlink)   DIRECT=0 ;;
        --force|-f)  FORCE=1 ;;
        --global|-g) GLOBAL=1 ;;
    esac
done

# --- Build ---
echo "[+] Building gh-pt..."
if ! make build; then
    echo -e "${RED_DOT} ${RED_TXT}BUILD FAILED${NC}"
    exit 1
fi
BUILT_BIN="$(pwd)/gh-pt"

# --- gh extension (always symlink) ---
EXTENSION_DIR="${HOME}/.local/share/gh/extensions/gh-pt"
if [ -L "${EXTENSION_DIR}" ]; then
    echo "[*] Symlink already exists at ${EXTENSION_DIR}."
else
    gh extension install .
fi

# Helper: check if the active binary on PATH is the newly installed one
active_is_new() {
    local active
    active="$(command -v gh-pt 2>/dev/null || true)"
    [ -z "$active" ] && return 1
    local resolved
    resolved="$(readlink -f "$active" 2>/dev/null || realpath "$active" 2>/dev/null || echo "$active")"

    # Determine expected location based on install mode
    local expected
    if [ "$DIRECT" -eq 1 ]; then
        # Direct copy: installed at BIN_TARGET
        expected="$(realpath "$BIN_TARGET" 2>/dev/null || echo "$BIN_TARGET")"
    else
        # Symlink: installed points to BUILT_BIN
        expected="$(realpath "$BUILT_BIN" 2>/dev/null || echo "$BUILT_BIN")"
    fi
    [ "$resolved" = "$expected" ]
}

# Check if user-level install exists and would take precedence
user_install_exists() {
    [ -e "${HOME}/.local/bin/gh-pt" ] || [ -L "${HOME}/.local/bin/gh-pt" ]
}

# Check if system binary exists
system_binary_exists() {
    local active
    active="$(command -v gh-pt 2>/dev/null || true)"
    [ -n "$active" ] && [[ "$active" == /usr/* ]]
}

# --- Set BIN_TARGET and ALIAS_TARGET based on --global flag ---
if [ "$GLOBAL" -eq 1 ]; then
    BIN_DIR="/usr/local/bin"
else
    BIN_DIR="${HOME}/.local/bin"
fi
BIN_TARGET="${BIN_DIR}/gh-pt"
ALIAS_TARGET="${BIN_DIR}/ghpt"

# Determine if we have existing binaries at targets
target_exists=0
if [ "$GLOBAL" -eq 1 ]; then
    if sudo test -e "$BIN_TARGET" 2>/dev/null || sudo test -L "$BIN_TARGET" 2>/dev/null; then
        if sudo test -e "$ALIAS_TARGET" 2>/dev/null || sudo test -L "$ALIAS_TARGET" 2>/dev/null; then
            target_exists=1
        fi
    fi
else
    if [[ -e "$BIN_TARGET" || -L "$BIN_TARGET" ]] && [[ -e "$ALIAS_TARGET" || -L "$ALIAS_TARGET" ]]; then
        target_exists=1
    fi
fi

# Check precedence override
precedence_override=0
if user_install_exists && ! active_is_new; then
    precedence_override=1
fi

# Helper: check if target on disk matches new build
target_is_new() {
    local t="$1"
    if [ "$DIRECT" -eq 1 ]; then
        cmp -s "$BUILT_BIN" "$t"
    else
        local resolved
        resolved="$(readlink -f "$t" 2>/dev/null || realpath "$t" 2>/dev/null || echo "$t")"
        [ "$resolved" = "$BUILT_BIN" ]
    fi
}

targets_are_new() {
    target_is_new "$BIN_TARGET" && target_is_new "$ALIAS_TARGET"
}

# ========== INSTALL LOGIC ==========
if [ "$DIRECT" -eq 1 ]; then
    # --- Direct (hard copy) mode ---
    if [ "$target_exists" -eq 1 ] && [ "$FORCE" -eq 0 ] && targets_are_new; then
        echo -e "${GREEN_DOT} ${GREEN_TXT}BUILD SUCCESSFUL — INSTALLED SUCCESSFULLY (binary up-to-date)${NC}"
        exit 0
    fi

    # Force or no existing target - proceed with copy
    if [ "$GLOBAL" -eq 1 ]; then
        sudo rm -f "$BIN_TARGET" "$ALIAS_TARGET"
        sudo cp "$BUILT_BIN" "$BIN_TARGET"
        sudo cp "$BUILT_BIN" "$ALIAS_TARGET"
    else
        mkdir -p "$BIN_DIR"
        rm -f "$BIN_TARGET" "$ALIAS_TARGET"
        cp "$BUILT_BIN" "$BIN_TARGET"
        cp "$BUILT_BIN" "$ALIAS_TARGET"
    fi

    if targets_are_new; then
        echo -e "${GREEN_DOT} ${GREEN_TXT}BUILD SUCCESSFUL — INSTALLED SUCCESSFULLY (direct copy: gh-pt, ghpt)${NC}"
    else
        echo -e "${RED_DOT} ${RED_TXT}BUILD SUCCESSFUL — INSTALL FAILED: failed to copy to ${BIN_TARGET} or ${ALIAS_TARGET}${NC}"
        exit 1
    fi

else
    # --- Symlink mode (default) ---
    if [ "$target_exists" -eq 1 ] && [ "$FORCE" -eq 0 ] && targets_are_new; then
        echo -e "${GREEN_DOT} ${GREEN_TXT}BUILD SUCCESSFUL — INSTALLED SUCCESSFULLY (symlinks up-to-date)${NC}"
        exit 0
    fi

    if [ "$GLOBAL" -eq 1 ]; then
        sudo rm -f "$BIN_TARGET" "$ALIAS_TARGET"
        sudo ln -s "$BUILT_BIN" "$BIN_TARGET"
        sudo ln -s "$BUILT_BIN" "$ALIAS_TARGET"
    else
        mkdir -p "$BIN_DIR"
        rm -f "$BIN_TARGET" "$ALIAS_TARGET"
        ln -s "$BUILT_BIN" "$BIN_TARGET"
        ln -s "$BUILT_BIN" "$ALIAS_TARGET"
    fi

    if targets_are_new; then
        echo -e "${GREEN_DOT} ${GREEN_TXT}BUILD SUCCESSFUL — INSTALLED SUCCESSFULLY (gh-pt, ghpt)${NC}"
    else
        echo -e "${RED_DOT} ${RED_TXT}BUILD SUCCESSFUL — INSTALL FAILED: failed to link ${BIN_TARGET} or ${ALIAS_TARGET}${NC}"
        exit 1
    fi
fi

# Check for precedence override warning
if [ "$precedence_override" -eq 1 ]; then
    if system_binary_exists; then
        echo -e "${YELLOW_DOT} ${YELLOW_TXT}WARNING: Inactive new binary; precedence override by system binary${NC}"
    else
        echo -e "${YELLOW_DOT} ${YELLOW_TXT}WARNING: Inactive new binary; precedence override by user-level install${NC}"
    fi
fi