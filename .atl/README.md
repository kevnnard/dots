# .atl

Run `/skill-registry:refresh` after cloning or moving this repo to another system, before delegating work that loads skills.

## What is tracked

| File | Tracked | Notes |
|------|---------|-------|
| `skill-registry.md` | Yes | Snapshot of the project skill inventory. Skills themselves are not included. |
| `.skill-registry.cache.json` | No | Fingerprint cache; regenerated automatically. |

## Host-specific paths

The `SKILL.md` paths in `skill-registry.md` point to the host that generated it. They are an intentional runtime contract, not portable references. Do not hand-edit or normalize them; regenerate instead.
