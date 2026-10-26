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

# 1. Detect OS
OS="$(uname -s)"
case "$OS" in
    Darwin)
        echo -e "  ${GREEN}✓${RESET} Detected macOS system"
        ;;
    Linux)
        echo -e "  ${GREEN}✓${RESET} Detected Linux system"
        ;;
    *)
        echo -e "  ${YELLOW}⚠️  Operating system $OS detected (unsupported or untested)${RESET}"
        ;;
esac

# 2. Check for Git
if ! command -v git &>/dev/null; then
    echo -e "  ${YELLOW}⚠️  Git is not installed.${RESET}"
    if [ "$OS" = "Darwin" ]; then
        echo -e "  Installing Xcode Command Line Tools..."
        xcode-select --install
        echo -e "  ${YELLOW}Please finish Xcode installation and rerun this script.${RESET}"
        exit 1
    elif [ "$OS" = "Linux" ]; then
        if command -v apt-get &>/dev/null; then
            sudo apt-get update && sudo apt-get install -y git
        elif command -v pacman &>/dev/null; then
            sudo pacman -Sy --noconfirm git
        fi
    fi
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

if [ -f "$DOTS_DIR/bin/kevnnard-dots" ]; then
    cp "$DOTS_DIR/bin/kevnnard-dots" "$HOME/.local/bin/kevnnard-dots"
elif command -v go &>/dev/null; then
    echo -e "  ${BLUE}Compiling static Go binary...${RESET}"
    cd "$DOTS_DIR" && go build -o "$HOME/.local/bin/kevnnard-dots" ./cmd/dots
elif [ "$OS" = "Darwin" ] && command -v brew &>/dev/null; then
    echo -e "  ${BLUE}Installing via Homebrew Tap...${RESET}"
    brew tap kevnnard/tap
    brew install kevnnard/tap/kevnnard-dots
fi

ln -sf "$HOME/.local/bin/kevnnard-dots" "$HOME/.local/bin/dots"

# 5. Ensure ~/.local/bin is in PATH
if [[ ":$PATH:" != *":$HOME/.local/bin:"* ]]; then
    export PATH="$HOME/.local/bin:$PATH"
fi

echo -e "\n${BOLD}${GREEN}✨ Installation successful!${RESET}\n"
echo -e "Launching ${BOLD}kevnnard-dots${RESET} visual dashboard...\n"

# 6. Launch TUI
exec "$HOME/.local/bin/kevnnard-dots"
