#!/usr/bin/env bash
set -e

# ANSI Color codes
BOLD="\033[1m"
GREEN="\033[32m"
BLUE="\033[34m"
YELLOW="\033[33m"
CYAN="\033[36m"
RESET="\033[0m"

echo -e "${CYAN}"
cat << "EOF"
██╗  ██╗███████╗██╗   ██╗███╗   ██╗███╗   ██╗ █████╗ ██████╗ ██████╗ 
██║ ██╔╝██╔════╝██║   ██║████╗  ██║████╗  ██║██╔══██╗██╔══██╗██╔══██╗
█████╔╝ █████╗  ██║   ██║██╔██╗ ██║██╔██╗ ██║███████║██████╔╝██║  ██║
██╔═██╗ ██╔══╝  ╚██╗ ██╔╝██║╚██╗██║██║╚██╗██║██╔══██║██╔══██╗██║  ██║
██║  ██╗███████╗ ╚████╔╝ ██║ ╚████║██║ ╚████║██║  ██║██║  ██║██████╔╝
╚═╝  ╚═╝╚══════╝  ╚═══╝  ╚═╝  ╚═══╝╚═╝  ╚═══╝╚═╝  ╚═╝╚═╝  ╚═╝╚═════╝ 
                      DOTFILES & MACHINE PROVISIONER
EOF
echo -e "${RESET}"

echo -e "${BOLD}${BLUE}🚀 Initializing Kevnnard Dots Installer...${RESET}\n"

ORIG_PATH="$PATH"

# --- helpers ---------------------------------------------------------------
have() { command -v "$1" &>/dev/null; }

SUDO=""
if [ "$(id -u)" -ne 0 ] && have sudo; then SUDO="sudo"; fi

# Installs packages with the native package manager of this machine.
pm_install() {
    if have pacman; then
        $SUDO pacman -S --needed --noconfirm "$@"
    elif have apt-get; then
        $SUDO apt-get update && $SUDO apt-get install -y "$@"
    elif have dnf; then
        $SUDO dnf install -y "$@"
    elif have zypper; then
        $SUDO zypper install -y "$@"
    elif have apk; then
        $SUDO apk add "$@"
    elif have brew; then
        brew install "$@"
    else
        return 1
    fi
}

ensure_cmd() {
    local cmd="$1"
    if have "$cmd"; then return 0; fi
    echo -e "  ${YELLOW}⚠️  $cmd is not installed. Installing with the native package manager...${RESET}"
    if ! pm_install "$cmd"; then
        echo -e "  ${YELLOW}✗ Could not install $cmd automatically. Please install it and rerun.${RESET}"
        exit 1
    fi
}

# True when the installed Go is recent enough to fetch the toolchain go.mod asks for (>= 1.21).
go_is_usable() {
    have go || return 1
    local v minor
    v="$(go env GOVERSION 2>/dev/null | sed 's/^go//')"
    minor="$(echo "$v" | cut -d. -f2)"
    [ -n "$minor" ] && [ "$minor" -ge 21 ] 2>/dev/null
}

# Downloads the official Go toolchain into ~/.local/go (no root needed).
install_go() {
    local goos goarch ver
    case "$(uname -s)" in
        Darwin) goos="darwin" ;;
        Linux)  goos="linux" ;;
        *) echo "unsupported OS for Go download"; return 1 ;;
    esac
    case "$(uname -m)" in
        x86_64|amd64)  goarch="amd64" ;;
        aarch64|arm64) goarch="arm64" ;;
        *) echo "unsupported architecture $(uname -m)"; return 1 ;;
    esac
    ensure_cmd curl
    ver="$(curl -fsSL 'https://go.dev/VERSION?m=text' | head -n1)"
    echo -e "  ${BLUE}Downloading Go toolchain ${ver} (${goos}/${goarch})...${RESET}"
    rm -rf "$HOME/.local/go"
    mkdir -p "$HOME/.local"
    curl -fsSL "https://go.dev/dl/${ver}.${goos}-${goarch}.tar.gz" | tar -C "$HOME/.local" -xz
    export PATH="$HOME/.local/go/bin:$PATH"
}

# 1. Detect OS
OS="$(uname -s)"
case "$OS" in
    Darwin) echo -e "  ${GREEN}✓${RESET} Detected macOS system" ;;
    Linux)  echo -e "  ${GREEN}✓${RESET} Detected Linux system" ;;
    *)
        echo -e "  ${YELLOW}✗ $OS is not supported by this script. On Windows use WSL, or build with 'go build ./cmd/dots'.${RESET}"
        exit 1
        ;;
esac

# 2. Check for Git
if ! have git; then
    if [ "$OS" = "Darwin" ]; then
        echo -e "  Installing Xcode Command Line Tools..."
        xcode-select --install || true
        echo -e "  ${YELLOW}Please finish Xcode installation and rerun this script.${RESET}"
        exit 1
    fi
    ensure_cmd git
fi
echo -e "  ${GREEN}✓${RESET} Git is available"

# 3. Target Clone Directory and Repository Source
REPO_URL="${DOTS_REPO:-https://github.com/kevnnard/dots.git}"
DOTS_DIR="$HOME/.dotfiles"
if [ -d "$HOME/Repositories" ]; then
    DOTS_DIR="$HOME/Repositories/dots"
fi

if [ ! -d "$DOTS_DIR" ]; then
    echo -e "\n${BOLD}📦 Cloning dotfiles repository from ${REPO_URL} to ${DOTS_DIR}...${RESET}"
    git clone "$REPO_URL" "$DOTS_DIR"
else
    echo -e "  ${GREEN}✓${RESET} Dotfiles repository found at: ${DOTS_DIR}"
fi

# 4. Install / Build Binary
mkdir -p "$HOME/.local/bin"
export PATH="$HOME/.local/bin:$HOME/.local/go/bin:$PATH"

if ! go_is_usable; then
    install_go
fi
echo -e "  ${BLUE}Compiling static Go binary...${RESET}"
(cd "$DOTS_DIR" && go build -o "$HOME/.local/bin/kevnnard-dots" ./cmd/dots)

if [ ! -x "$HOME/.local/bin/kevnnard-dots" ]; then
    echo -e "  ${YELLOW}✗ Could not produce the kevnnard-dots binary.${RESET}"
    exit 1
fi
ln -sf "$HOME/.local/bin/kevnnard-dots" "$HOME/.local/bin/dots"

# 5. Make sure the shell can find it next time
if [[ ":${ORIG_PATH:-}:" != *":$HOME/.local/bin:"* ]]; then
    echo -e "  ${YELLOW}ℹ Add this to your shell profile:${RESET} export PATH=\"\$HOME/.local/bin:\$PATH\""
fi

echo -e "\n${BOLD}${GREEN}✨ Installation successful!${RESET}\n"

# 6. Launch TUI (set DOTS_NO_LAUNCH=1 to skip)
if [ -n "${DOTS_NO_LAUNCH:-}" ]; then
    exit 0
fi
echo -e "Launching ${BOLD}kevnnard-dots${RESET} visual dashboard...\n"
exec "$HOME/.local/bin/kevnnard-dots"
