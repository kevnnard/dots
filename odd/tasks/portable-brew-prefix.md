# Portable Homebrew prefix in backed-up configs

Backed-up config text that names the source machine's Homebrew prefix (for example `/opt/homebrew/bin/engram`) must be restored with the destination machine's prefix, the same way `__HOME__` already handles the home directory. Branch `fix/portable-brew-prefix`, stacked on `fix/portable-package-links`.

## Specs

S1. User: "Dale arranca entonces cuando termianes ultimas verificacion par aque todo todo este listo" — accepting the proposal: "hacer con `/opt/homebrew` lo mismo que ya se hace con la carpeta personal. Al hacer el backup se reemplaza por un marcador como `__BREW_PREFIX__`, y al restaurar se pone el prefijo real de Homebrew en esa máquina".
S2. Generic, not per app: "no solo con ese si no con cualquier app depoendica nueva" (L1 of portable-package-links). Every backed-up text file goes through the same transformation.
S3. Prefix evidence is independently verified: `HOMEBREW_PREFIX` when set, otherwise `brew --prefix`; the prefix must be an absolute directory holding `bin/brew`. No guessed or hardcoded prefixes.
S4. Proposal: "si en destino no existe, dots avisa en vez de dejarla rota" — a destination without a verified prefix keeps the `__BREW_PREFIX__` token and the restore reports a warning for that file.
S5. Same rules as `__HOME__`: whole path prefixes only, binary and >8 MiB files untouched, equivalence checks (restore and status) normalize both tokens so equivalent files are not reported as changed.
S6. Then repeat the final verifications: "cuando termianes ultimas verificacion par aque todo todo este listo" — new Mac backup, send to Linux, restore, compare.

## Tasks

T1 [pending] S1-S5 | route: inline | homepath brew token, restore warning, tests, docs | commit: -
T2 [pending] S6 | route: parent, SSH | new backup, transfer, restore with candidate, comparison | commit: operational evidence only

## Log

L1. User request (accepting the proposal above): "Dale arranca entonces cuando termianes ultimas verificacion par aque todo todo este listo".
L2. Evidence: Mac stage files with `/opt/homebrew`: codex `config.toml` (`command = "/opt/homebrew/bin/engram"`), nvim `nodejs.lua` candidate list, herdr heeler `runtime.json`/`runtime-node` (versioned Cellar node path, runtime state), `.zshrc` (`BREW_BIN="/opt/homebrew/bin"`), codex system skill script. Linux: `brew --prefix` = `/home/linuxbrew/.linuxbrew`; engram and node exist there.
L3. Non-goals: versioned `Cellar/<pkg>/<version>` paths stay version-specific; `~/Library/...` paths have no Linux equivalent and are not rewritten; legacy backups with literal `/opt/homebrew` are not rewritten (no blind substitution).
L4. Engram mirror unavailable in this session (dispatch fails); this file is the authoritative record.
