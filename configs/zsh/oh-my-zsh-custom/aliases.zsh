# ============================================================================
# KEVNNARD CUSTOM ALIASES
# ============================================================================

# ---------------------
# PNPM Aliases
# ---------------------
alias p="pnpm"
alias pi="pnpm install"
alias pa="pnpm add"
alias pad="pnpm add -D"
alias pr="pnpm run"
alias pd="pnpm dev"
alias pb="pnpm build"
alias pt="pnpm test"
alias pl="pnpm lint"
alias pf="pnpm format"
alias pc="pnpm clean"
alias pck="pnpm check"
alias pw="pnpm -w"
alias pwi="pnpm -w install"
alias pwr="pnpm -w run"

# Turbo shortcuts
alias tb="pnpm turbo build"
alias td="pnpm turbo dev"
alias tt="pnpm turbo test"
alias tl="pnpm turbo lint"
alias tc="pnpm turbo clean"

# ---------------------
# BUN Aliases
# ---------------------
alias b="bun"
alias bi="bun install"
alias ba="bun add"
alias bad="bun add -d"
alias br="bun run"
alias bd="bun dev"
alias bb="bun build"
alias bt="bun test"
alias bx="bunx"

# ---------------------
# Quick Navigation
# ---------------------
alias ..="cd .."
alias ...="cd ../.."
alias ....="cd ../../.."
alias ~="cd ~"

# ---------------------
# Utilities
# ---------------------
alias ll="ls -la"
alias cls="clear"
alias reload="source ~/.zshrc"
alias zshconfig="code ~/.zshrc"
alias ohmyzsh="code ~/.oh-my-zsh"

# ---------------------
# Development
# ---------------------
alias serve="python3 -m http.server 8000"
alias ports="lsof -i -P -n | grep LISTEN"
alias myip="curl -s ifconfig.me"

# ---------------------
# Git extras (adicionales a los de Oh My Zsh)
# ---------------------
alias glog="git log --oneline --graph --decorate"
alias gundo="git reset HEAD~1"
alias greset="git reset --hard HEAD"

# LAZY ALIASES
alias lg="lazygit"
alias ldd="lazydocker"

# DOCKER ALIASES
alias docker-up="open /Applications/Docker.app"
alias dci="docker container init"

# VIM ALIASES
alias vmd="nvim"
alias vm="nvim ."

alias ZTR="zellij --layout ~/.config/zellij/layouts/tr .kdl"
alias ZQA="zellij --layout ~/.config/zellij/layouts/qa.kdl"

# Gentle Shell
alias gsl='gentle-shell'
alias ga='gentle-ai'
alias gu='gentle-ai sync; gentle-shell update --extensions'

# Terminal Browser  
alias tb='terminal-browser'

# ---------------------
# Herdr Remote Helper (hr)
# ---------------------
hr() {
  local direction="right"
  local in_place=0
  local full_remote=0
  local target=""

  while [[ $# -gt 0 ]]; do
    case "$1" in
      -c|--current|--here)
        in_place=1
        shift
        ;;
      -d|--down)
        direction="down"
        shift
        ;;
      -r|--right)
        direction="right"
        shift
        ;;
      -f|--full)
        full_remote=1
        shift
        ;;
      -h|--help)
        echo "Uso: hr [opciones] [host|ip]"
        echo "  Conecta a una máquina remota en un pane de Herdr (default: kevnnard-station)"
        echo ""
        echo "Opciones:"
        echo "  -r, --right    Abre el split a la derecha (default)"
        echo "  -d, --down     Abre el split abajo"
        echo "  -c, --current  Conecta en el pane actual sin hacer split"
        echo "  -f, --full     Conecta todo Herdr al servidor remoto (herdr --remote)"
        return 0
        ;;
      *)
        target="$1"
        shift
        ;;
    esac
  done

  target="${target:-kevnnard-station}"
  [[ "$target" == "linux" || "$target" == "station" ]] && target="kevnnard-station"

  # Conectar sesión completa de Herdr
  if [[ $full_remote -eq 1 ]]; then
    herdr --remote "$target"
    return $?
  fi

  # Split interactivo en Herdr si estamos dentro del workspace
  if [[ $in_place -eq 0 ]] && command -v herdr >/dev/null 2>&1 && herdr pane current >/dev/null 2>&1; then
    local new_pane
    new_pane=$(herdr pane split --current --direction "$direction" --focus 2>/dev/null | jq -r '.result.pane.pane_id // empty' 2>/dev/null)
    if [[ -n "$new_pane" ]]; then
      herdr pane run "$new_pane" "ssh $target"
      return 0
    fi
  fi

  # Conectar directamente en la terminal actual
  ssh "$target"
}
