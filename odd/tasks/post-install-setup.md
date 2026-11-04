# post-install-setup
Feature: success summary after install and guided `dots setup`. Status: planned.
Scope: dots reports and guides only. It never reads, stores, or logs tokens or credentials.
Non-goals: credential storage, token validation, OAuth handling.

## Specs
S1. When the install finishes successfully, dots prints a success summary: what was installed, what was configured, and what is still pending. Source request: "si la istalcion fue exitosa salga el succes con todo lo hecho".
S2. `dots setup` runs its steps in priority order: steps that need no token first (zsh with powerlevel10k), then steps that need a login (claude, copilot, and others). Source request: "vamos paso a paso por orden de jerarquia configurando los que requieran tokens", example "primero configuraríamos zsh powerlevel10k despues claude copilot etc".
S3. For each login step, dots shows the exact command and runs it in the same interactive terminal, so password prompts and login links appear there. The user completes them. Source request: "se corre en la misma interactiva ... ahi saldra si requiere passrwod etc y que el link y abrirlo".
S4. dots verifies a login step only through the tool's own status command (for example `gh auth status`). dots does not validate tokens itself.
S5. `dots setup` waits for the user to finish each login step before it moves to the next one.
S6. `dots setup` is a separate command, and it can resume after the last completed step.
S7. The first setup step lets the user choose the default login shell, offering zsh and bash for now (more shells can be added later, from `/etc/shells`). dots runs `chsh -s <shell>` in the interactive terminal, the user types any password, and dots then verifies the change with `getent passwd`. The change applies in the next session, and the summary says so. Source request: "poner zsh como prioicpal antes que bash o algun otro".

## Tasks
T1. Success summary after install (S1). Route: inline. Commit: pending.
T2. `dots setup` skeleton with the ordered step list (S2, S6). Route: inline. Commit: pending.
T3. Default shell step, zsh or bash, with chsh and getent verification (S7). Route: inline. Commit: pending.
T3b. zsh + powerlevel10k step, no token (S2). Route: inline. Commit: pending.
T4. Login steps with interactive run and status check, starting with claude and copilot (S3, S4, S5). Route: inline. Commit: pending.
T5. Docs for `dots setup` and the success summary. Route: inline. Commit: pending.

## Log
L1. Original request (Spanish, verbatim): "dale entonces crea eso de una y me avisas" with the prior requirements in S1 to S6, and "se corre en la misma interactiva osea la ocrres tu y bueno ahi saldra si requiere passrwod etc y que el link y abrirlo etc".
L2. Clarification: dots only offers the exact login command, the user runs it, and the credential stays in the system, not in dots.
