# Feature: Dots CLI
A Go-based cross-platform dotfiles and workstation setup manager for macOS and Linux.

## Specs
- S1: "vamos a contruir un progrma de termianl par guardar mi dots osea toa mi configuraciond e mac programar configuracionde programasas como nvim, PI , pnpm brew dependecias esencales configs esecnviales"
- S2: "la aplicacion debera detectar a que sistema va enfoncado la nueva iinstlaaccion"
- S3: Manifest-driven architecture with `manifest.yaml` declaring tracked apps, paths, exclusions, and OS-specific install recipes.
- S4: "vamos a ccopiar los paths que vayasmos eleigiendo segunq ue queremos guardar que app que connfigs etc" (backup command copies configs to dotfiles repo, sanitizes secrets, dumps Brewfile on macOS).
- S5: "si reseteo mi mac pueda tener mii mismca config que lograo alo largo de mi carrea y pues mantener mi setup ppero con una pc mas limppia o si llegase a cambaro de pc no solo para mac" (restore command detects OS, installs packages, and copies configs).
- S6: "ya lo que son envs o secretos debers crear nuevas eso ssi" (secrets/env templates: never commit raw secrets, generate .env.template / notify user).
- S7: Management commands: `dots add` to track new apps/paths, `dots status` for drift detection.
- S8: "docker si caddy mkcert opcioneales osea dar opcionn de intarlalos ppor defecto no... open jd pues ppor que usamos react antive pero para creas build nnativos neceaii coappods igual eso iria en config para desarroolo de mobiile appss osea otro paso pero si van... dotnet iria con el desarrolo web obligatoria tambien" (Profiles: core, mobile, infra).

## Tasks
- T1 (S2): OS, architecture, and package manager detector package (`pkg/detector`) with unit tests. [DONE]
- T2 (S3): Manifest domain model and YAML parser (`pkg/manifest`) with path resolution and default app templates. [DONE]
- T3 (S4, S6): Backup engine (`pkg/backup`) with file copying, exclusions, secret protection, and Brewfile export. [DONE]
- T4 (S5): Restore engine (`pkg/restore`) with OS-specific package provisioning and file deployment. [DONE]
- T5 (S7): Status and diff engine (`pkg/status`) and app addition helper. [DONE]
- T6 (S1, S7): Main CLI command line interface (`cmd/dots`) wiring all subcommands (`backup`, `restore`, `status`, `add`, `init`). [DONE]
- T7 (S1-S7): End-to-end verification, tests, and documentation. [DONE]
- T8 (S8): Add profile filtering (`core`, `mobile`, `infra`, `all`) to manifest and CLI.
- T9 (S8): Curate the user's specific Brewfiles and apps according to core, mobile, and infra profiles.

## Log
- L1: User requested a terminal program in Go to save macOS and cross-platform configurations (nvim, PI, pnpm, brew, essential dependencies and configs), detect target OS on clean install/new PC, copy selected paths, handle secrets safely, and provide backup/install systems.
- L2: Agreed architecture: Go binary, manifest-driven in `~/.dotfiles`, repository in `~/Repositories/dots`.
- L3: User defined profiles: core (default with shell, dev tools, web dev, dotnet, docker, fonts, gui), mobile (React Native: openjdk@17, cocoapods, fastlane), infra (optional: caddy, mkcert, act).
