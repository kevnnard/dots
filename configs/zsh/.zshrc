# Enable Powerlevel10k instant prompt. Should stay close to the top of ~/.zshrc.
# Initialization code that may require console input (password prompts, [y/n]
# confirmations, etc.) must go above this block; everything else may go below.
if [[ -r "${XDG_CACHE_HOME:-$HOME/.cache}/p10k-instant-prompt-${(%):-%n}.zsh" ]]; then
  source "${XDG_CACHE_HOME:-$HOME/.cache}/p10k-instant-prompt-${(%):-%n}.zsh"
fi

export ZSH="$HOME/.oh-my-zsh"
export PATH="$HOME/.local/bin:$HOME/.opencode/bin:$HOME/.cargo/bin:$HOME/.volta/bin:$HOME/.bun/bin:$HOME/.nix-profile/bin:/nix/var/nix/profiles/default/bin:/usr/local/bin:$PATH"

# Set nvim as default editor for opencode and other tools
export EDITOR="nvim"
export VISUAL="nvim"

if [[ $- == *i* ]]; then
    # Commands to run in interactive sessions can go here
fi

export LS_COLORS="di=38;5;67:ow=48;5;60:ex=38;5;132:ln=38;5;144:*.tar=38;5;180:*.zip=38;5;180:*.jpg=38;5;175:*.png=38;5;175:*.mp3=38;5;175:*.wav=38;5;175:*.txt=38;5;223:*.sh=38;5;132"
# Colored ls: GNU ls on Linux, gls (coreutils) on macOS when available
if [[ "$(uname)" == "Darwin" ]]; then
  (( $+commands[gls] )) && alias ls='gls --color=auto'
else
  alias ls='ls --color=auto'
fi

# Homebrew (optional: only if installed)
for _brew in /opt/homebrew/bin/brew /usr/local/bin/brew /home/linuxbrew/.linuxbrew/bin/brew "$HOME/.linuxbrew/bin/brew"; do
  if [[ -x "$_brew" ]]; then
    eval "$("$_brew" shellenv)"
    break
  fi
done
unset _brew
BREW_PREFIX="${HOMEBREW_PREFIX:-}"

# Source a zsh plugin from brew prefix or common system locations
_source_plugin() {
  local f
  for f in "$@"; do
    [[ -r "$f" ]] && { source "$f"; return 0; }
  done
  return 1
}
_source_plugin \
  "$BREW_PREFIX/share/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh" \
  /usr/share/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh \
  /usr/share/zsh/plugins/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh
_source_plugin \
  "$BREW_PREFIX/share/zsh-autosuggestions/zsh-autosuggestions.zsh" \
  /usr/share/zsh-autosuggestions/zsh-autosuggestions.zsh \
  /usr/share/zsh/plugins/zsh-autosuggestions/zsh-autosuggestions.zsh
_source_plugin \
  "$BREW_PREFIX/share/powerlevel10k/powerlevel10k.zsh-theme" \
  "$HOME/powerlevel10k/powerlevel10k.zsh-theme" \
  /usr/share/zsh-theme-powerlevel10k/powerlevel10k.zsh-theme \
  /usr/share/powerlevel10k/powerlevel10k.zsh-theme
unset -f _source_plugin

export PROJECT_PATHS="$HOME/Repositories"
export FZF_DEFAULT_COMMAND="fd --hidden --strip-cwd-prefix --exclude .git"
export FZF_DEFAULT_T_COMMAND="$FZF_DEFAULT_COMMAND"
export FZF_ALT_COMMAND="fd --type=d --hidden --strip-cwd-prefix --exclude .git"



# alias
alias fzfbat='fzf --preview="bat --theme=gruvbox-dark --color=always {}"'
alias fzfnvim='nvim $(fzf --preview="bat --theme=gruvbox-dark --color=always {}")'

#plugins
plugins=(
  command-not-found docker docker-compose brew 
)

[[ -r "$ZSH/oh-my-zsh.sh" ]] && source "$ZSH/oh-my-zsh.sh"

export CARAPACE_BRIDGES='zsh,fish,bash,inshellisense'
zstyle ':completion:*' format $'\e[2;37mCompleting %d\e[m'
(( $+commands[carapace] )) && source <(carapace _carapace)

(( $+commands[fzf] ))    && eval "$(fzf --zsh)"
(( $+commands[atuin] ))  && eval "$(atuin init zsh)"
(( $+commands[zoxide] )) && eval "$(zoxide init zsh)"

function share() {
  domain="epizoutically-unfomented-lucas.ngrok-free.dev"

  if [[ $# -eq 0 ]]; then
    currentFolder=${PWD##*/}
    ngrok http "${currentFolder}:443" --host-header=rewrite --domain="$domain"
    return
  fi

  if [[ $# -gt 1 ]]; then
    ngrok http "localhost:$1" --host-header=rewrite --domain="$domain"
  else
    ngrok http "localhost:$1" --host-header=rewrite --domain="$domain"
  fi
}



# pnpm
if [[ "$(uname)" == "Darwin" ]]; then
  export PNPM_HOME="$HOME/Library/pnpm"
else
  export PNPM_HOME="${XDG_DATA_HOME:-$HOME/.local/share}/pnpm"
fi
case ":$PATH:" in
  *":$PNPM_HOME:"*) ;;
  *) export PATH="$PNPM_HOME:$PATH" ;;
esac
# pnpm end

# Android SDK
if [[ "$(uname)" == "Darwin" ]]; then
  export ANDROID_HOME="$HOME/Library/Android/sdk"
else
  export ANDROID_HOME="$HOME/Android/Sdk"
fi
export PATH="$ANDROID_HOME/platform-tools:$PATH"

# To customize prompt, run `p10k configure` or edit ~/.p10k.zsh.
[[ ! -f ~/.p10k.zsh ]] || source ~/.p10k.zsh

