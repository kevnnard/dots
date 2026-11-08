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

T1 [done] S1-S5 | route: inline | homepath brew token, restore warning, tests, docs | commit: 9ed05a8
T2 [done] S6 | route: parent, SSH | new backup, transfer, restore with candidate, comparison | commit: operational evidence only

## Log

L1. User request (accepting the proposal above): "Dale arranca entonces cuando termianes ultimas verificacion par aque todo todo este listo".
L2. Evidence: Mac stage files with `/opt/homebrew`: codex `config.toml` (`command = "/opt/homebrew/bin/engram"`), nvim `nodejs.lua` candidate list, herdr heeler `runtime.json`/`runtime-node` (versioned Cellar node path, runtime state), `.zshrc` (`BREW_BIN="/opt/homebrew/bin"`), codex system skill script. Linux: `brew --prefix` = `/home/linuxbrew/.linuxbrew`; engram and node exist there.
L3. Non-goals: versioned `Cellar/<pkg>/<version>` paths stay version-specific; `~/Library/...` paths have no Linux equivalent and are not rewritten; legacy backups with literal `/opt/homebrew` are not rewritten (no blind substitution).
L5. T1 evidence: RED homepath (build failure: BrewPrefix/Unresolved undefined) and restore (2 failures: no warning without prefix) observed; GREEN focused; backup/status integration tests passed on first run (behavior flows through homepath.Tokenize/Equivalent, pinned as regression tests). go vet, build, go test ./... 400 passed in 13 packages, also with HOMEBREW_PREFIX=/nonexistent; diff --check clean.
L6. T2: new Mac stage /Users/kevnnard/.cache/dots/brew-refresh-20261108-145610.ehc4/stage (backup exit0, 2178 processed; 6 files carry __BREW_PREFIX__, only manifest.yaml keeps literal /opt/homebrew). tar over SSH to /home/kevnnard/.cache/dots/incoming/ssh-brew-20261108-145610/stage; same fingerprint both ends {f2169,l10,d648} 61d9e746...; new herdr usagebar plugin checkout explains extra files. Linux candidate SHA256 5ec1bd1c... Restore (`restore --all --with-config --install-packages --replace-existing`) exit1 only for the same three known false-negative installs; 9 safety backups; no 'left unexpanded' warning (brew --prefix /home/linuxbrew/.linuxbrew). Audit: 2160 match, 629 dirs, 7 links, 3 package links, 7 templates, 55 absent-in-stage, 1 .zshrc diff = logged repair (9 guarded lines). Live: codex engram command /home/linuxbrew/.linuxbrew/bin/engram (executable), nvim nodejs.lua and .zshrc BREW_BIN use linuxbrew, zero leftover __BREW_PREFIX__; herdr runtime.json Cellar node path checked separately; models 20/1/1 both files.
L7. Independent verify mw8998hn-d-xvzu: S1-S4 PASS; vet/build/test pass with and without prefix; differential base b05266f vs HEAD for status and backup (no brew prefix, also HOMEBREW_PREFIX=/nonexistent) identical stdout/stderr/exit/staged bytes. S5 flagged: repaired content (writeExpanded) and Equivalent ignore the 8 MiB limit. Parent decision: pre-existing behavior shared with __HOME__ since the repair path existed (S5 asks for the same rules as __HOME__), so not changed here; changing it would alter unrequested __HOME__ behavior. Compatibility note added to docs: older dots copies __BREW_PREFIX__ verbatim. S6 PARTIAL only for the three known installer false negatives (pacman -Q evidence L36/L39 of portable-package-links).
L4. Engram mirror unavailable in this session (dispatch fails); this file is the authoritative record.
