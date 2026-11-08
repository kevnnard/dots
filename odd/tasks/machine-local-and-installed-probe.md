# Machine-local settings and installed-package detection

Two restore fixes on branch `fix/machine-local-settings` (from main `dc3372c`, v1.5.8): per-machine files keep their local content, and packages already installed through the destination's package manager are not reinstalled.

## Specs

S1. User (relayed report): "Ojo con tus dots: volvieron a cambiar la clave de firma de git a la del Mac (2E743206B31593A1) y el commit fallaba. La dejé otra vez en la clave de Linux, con respaldo de ~/.gitconfig. Es la misma restauración que te cambia los subagentes a minimax. Conviene que tus dots no pisen ~/.gitconfig ni subagents.json en Linux. debe mantener su fruma cada uno".
S2. Restore never replaces an existing `~/.gitconfig`, `~/.pi/agent/subagents.json` or `~/.gentle-shell/agent/subagents.json`, even with `--replace-existing`; it reports them as kept. A missing file is still deployed (first setup). Generic: a manifest mapping flag `keep_local: true`, also honored for older local manifests through the shipped inventory.
S3. User: "lo de los 3 fallos que es que si ya isntalado omita la instalcion no es asi ?" + "si". If the selected recipe's package is already installed per the destination's package manager (for example `pacman -Q`, `brew list`, `dpkg -s`), restore marks it installed and runs no install (no sudo). Generic for any app; unknown cases keep today's behavior.
S4. Verify on Linux: new backup, transfer, restore; `~/.gitconfig` and both `subagents.json` unchanged; bitwarden and both fonts reported installed; no install failures from them.

## Tasks

T1 [done] S1-S2 | route: inline | keep_local flag, Effective merge, restore, manifest, docs, tests | commit: -
T2 [done] S3 | route: explore (mw8eg4dk-e-rp5z) then inline | package-manager receipt probe | commit: -
T3 [done] S4 | route: parent, SSH | Linux verification | commit: evidence only

## Log

L1. User request: "si y ojo con esto Ojo con tus dots: volvieron a cambiar la clave de firma de git a la del Mac (2E743206B31593A1) y el commit fallaba. La dejé otra vez en la clave de Linux, con respaldo de ~/.gitconfig. Es la misma restauración que te cambia los subagentes a minimax. Conviene que tus dots no pisen ~/.gitconfig ni subagents.json en Linux. debe mantener su fruma cada uno" (the "si" approves S3).
L2. Design: keep_local forces Replace=false for that mapping only; the existing safe keep path reports the file. Backup still stages the file so a new machine gets a starting copy.
L4. T1 RED: build failure (MachineLocal undefined) and manifest keep_local tests failing; GREEN restore/manifest; go vet + go test ./... pass. Help text, README, docs updated.
L5. T2 explore: clean.NativeProbe queries all recipes (brew/pacman/npm/pnpm, private query/listed helpers, no apt); recommended receipt check after resolveRecipe in planStep with the real provided map, selected recipe only, bounded timeout, unknown = today's behavior.
L6. T2 RED: clean/restore build failures (Installed, Receipts undefined), CLI test ran zsh despite pacman receipt; GREEN after wiring installReceipts(sys) (clean.NativeProbe.Installed via cleanExecutor) in CLI and TUI RunTUI. apt has no tested query: stays unknown -> today's behavior. go vet + go test ./... pass.\nL7. T3: Mac stage /Users/kevnnard/.cache/dots/local-refresh-20261108-172841.luGE/stage, Linux /home/kevnnard/.cache/dots/incoming/ssh-local-20261108-172841/stage, same fingerprint 97ab442c... (f2169 l10 d648); candidate sha 235fd082... Dry-run then real `restore --all --with-config --install-packages --replace-existing`: exit 0, "Finished without problems."; bitwarden, font-caskaydia-cove, font-iosevka-term "already installed, skipped"; "kept machine-specific file: /home/kevnnard/.gitconfig"; sha256sum -c of .gitconfig and both subagents.json OK (subagents already identical to backup); signingkey still the Linux key; no new .gitconfig .bk. 13 readiness items remain.\nL8. Independent verify mw8ene5r-f-fw7d: S1 PASS, S4 PASS (before.sha OK, real.log lines 10,34-35,552,2303); vet/test/build exit 0 on HEAD and dc3372c; differential restore/clean dry-run byte-identical. Findings kept as known limits (not user requirements): identical machine-local files are reported as restored (nothing written); keep_local symlink mappings are kept but not labelled; Effective mutating a local Install map is pre-existing; npm recipes also accept a pnpm receipt (clean's existing query); receipt queries have no timeout (clean's existing executor).\nL3. Engram mirror unavailable in this session; this file is authoritative.
