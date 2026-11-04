# Feature: Portable install (OS and package-manager agnostic)
Make `dots` work on any machine (macOS, Arch/Omarchy, Debian/Ubuntu, Fedora, Windows) without depending on the author's Mac paths or Homebrew.

## Specs
- S1: "dots no debe depender de mi sistemas si no estar listo para todos" (no hardcoded `/Users/kevnnard`, no assumption of brew or macOS).
- S2: "el linux que tengo es omarchy que creo es yay" (detect Arch derivatives such as Omarchy via `ID`/`ID_LIKE`; prefer yay, then pacman).
- S3: "algunos paquetes que no estas en brew para linux estan directamtne con linux o imagina con windopwsa" (per-manager install recipes: brew, yay/pacman, apt, dnf, zypper, winget/scoop/choco).
- S4: "si algunas nativas con curl o con el sistema opopetartiivo eso se debe rverificar" (curl/script recipes remain a fallback; verify what the OS provides natively before falling back).
- S5: Absolute home paths in backed-up configs are stored with a `__HOME__` placeholder and expanded on restore.
- S6: `install.sh` bootstraps git/curl/go with the native package manager when missing.

## Tasks
- T1 (S2,S3): Detector: parse `ID_LIKE`, Arch derivatives, zypper, Windows managers (winget, scoop, choco). [DONE] c3c4018
- T2 (S3,S4): Recipe resolution by manager (key = manager name, distro, family, OS, `default`), new command builders, Windows shell. [DONE] 0a2276c
- T2b (S3): `requires` field on apps; plans ordered so dependencies (node, pnpm) install before dependents (pi, claude, codex). [PENDING]
- T3 (S1,S3,S4): manifest.yaml: per-manager recipes, new node app, npm-based pi/claude/codex. [PENDING]
- T4 (S1,S5): `__HOME__` placeholder in backup/restore for absolute paths. [PENDING]
- T5 (S6): install.sh native bootstrap + README. [PENDING]

## Log
- L1: "tuvo muchos probelmas al intentar crear una instalacion en un linux sin backup es decir iuntalar pi , pnpppm etcetc zshh alaparecer eso creo ocomo la s rutas actuales del mac que es user/kevnnard ert cejemp[lo npro en linux es directamtne el user podemos revisar esopara corregirlo ?"
- L2: "claro y o solo zsh si no todas la dependencias recordar que si algunas nativas con curl o con el sistema opopetartiivo eso se debe rverificar por ejemppplo el linux que tengo es omarchy que creo es yay creo entonc es algunos paquetes que no estas en brew para linux estan directamtne con linux o imagina con windopwsa osea dots no debe depender de mi  sistemas si no estar listo para todos se entiden ?"
- L3: `configs/zsh/.zshrc` already made portable (uncommitted).
