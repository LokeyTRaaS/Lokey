# Releasing

LoKey versions are cut **automatically** from conventional commits on `main` using [semantic-release](https://semantic-release.gitbook.io/).

| Piece | Role |
|-------|------|
| [`.releaserc.json`](../.releaserc.json) | Commit analysis, changelog, git tag, GitHub Release notes |
| [`.github/workflows/release.yml`](../.github/workflows/release.yml) | Runs semantic-release on every push to `main` |
| [`.github/workflows/build.yml`](../.github/workflows/build.yml) | On `v*` tags: multi-arch binaries, GHCR manifests, release assets |
| [`package.json`](../package.json) | Pins semantic-release plugins (release tooling only; not an npm library) |
| [`CHANGELOG.md`](../CHANGELOG.md) | Updated by `@semantic-release/changelog` on each release |

## What happens on merge to `main`

```mermaid
flowchart LR
  merge[Merge to main] --> sr[semantic-release]
  sr -->|feat/fix/breaking| tag["Tag vX.Y.Z + CHANGELOG + GitHub Release"]
  sr -->|docs/chore/ci only| skip[No version bump]
  tag --> build[Dispatch build.yml on tag]
  build --> ghcr[GHCR X.Y.Z / vX.Y.Z / latest]
  build --> bins[Attach lokey-*-{amd64,arm64}.tar.gz]
```

1. **Semantic Release** walks commits since the last `v*` tag.
2. If a bump is needed, it:
   - Updates `CHANGELOG.md`
   - Commits `chore(release): X.Y.Z [skip ci]`
   - Creates annotated tag `vX.Y.Z`
   - Opens / updates the GitHub Release with generated notes
3. The workflow then **dispatches** `build.yml` for that tag so GHCR images and binary archives are published and attached to the same release.

Pushes whose commit message contains `[skip ci]` (including the release commit itself) do not re-enter the release workflow.

## Commit → version bump

Use [Conventional Commits](https://www.conventionalcommits.org/) on PRs merged to `main`:

| Commit | Semver |
|--------|--------|
| `fix: …` | **patch** (`1.2.0` → `1.2.1`) |
| `feat: …` | **minor** (`1.2.0` → `1.3.0`) |
| `feat!: …` or footer `BREAKING CHANGE:` | **major** (`1.2.0` → `2.0.0`) |
| `docs:`, `chore:`, `ci:`, `refactor:`, `test:`, `style:` (no breaking footer) | **no release** |

Examples:

```bash
git commit -m "feat: expose emulator flag on controller health"
git commit -m "fix: restore cAdvisor metrics under Docker 29"
git commit -m "feat!: rename status controllers map shape

BREAKING CHANGE: /api/v1/status controllers is now an object per device."
```

## Repo secret: `RELEASE_TOKEN`

GitHub’s default `GITHUB_TOKEN` cannot start new workflows from events it creates (tag push / bot commit). Set a dedicated PAT so automated releases are reliable:

1. Create a fine-grained PAT (or classic) with access to this repository:
   - **Resource owner:** `LokeyTRaaS`, repository `Lokey` only
   - **Contents:** Read and write (tags, release commit, GitHub Release)
   - **Actions:** Read and write (dispatch `build.yml`)
   - **Expiration:** **≤ 366 days** — the LokeyTRaaS org rejects fine-grained tokens with a longer lifetime
2. Add it as a **repository** secret named exactly **`RELEASE_TOKEN`**:  
   Settings → Secrets and variables → Actions → New repository secret  
   (repo-only; not an org-wide secret).
3. The release workflow uses `RELEASE_TOKEN` when present, otherwise falls back to `GITHUB_TOKEN` and still tries to `gh workflow run build.yml --ref vX.Y.Z`.

Rotate the PAT before it expires or if it leaks; do not commit it. Branch protection on `main` must allow the token’s identity (or admins) to push the `chore(release)` commit and tags.

If semantic-release fails with `organization forbids access via a fine-grained personal access tokens if the token's lifetime is greater than 366 days`, recreate the PAT with a shorter expiry and update the `RELEASE_TOKEN` secret.

## Consuming a release

**Containers (prefer pinning a semver, not only `latest`):**

```bash
export VER=1.2.0   # from https://github.com/LokeyTRaaS/Lokey/releases
# in .env:
# LOKEY_IMAGE_API=ghcr.io/lokeytraas/lokey/api:${VER}
# LOKEY_IMAGE_CONTROLLER=ghcr.io/lokeytraas/lokey/controller:${VER}
# LOKEY_IMAGE_FORTUNA=ghcr.io/lokeytraas/lokey/fortuna:${VER}
# LOKEY_IMAGE_VIRTIO=ghcr.io/lokeytraas/lokey/virtio:${VER}
docker compose pull && docker compose up -d
```

Multi-arch tags published per release: `X.Y.Z`, `vX.Y.Z`, `latest`, plus arch-specific `X.Y.Z-amd64` / `X.Y.Z-arm64`.

**Binaries:** download `lokey-<service>-<arch>.tar.gz` from the [Releases](https://github.com/LokeyTRaaS/Lokey/releases) page.

## Manual tag (escape hatch)

If you need to cut a version without waiting for semantic-release:

```bash
git tag -a v1.2.1 -m "Release v1.2.1"
git push origin v1.2.1
```

That still runs `build.yml` for GHCR + archives. Prefer letting semantic-release own version numbers so `CHANGELOG.md` stays consistent.

## Local dry-run

Release tooling needs Node.js >= 26.13.

```bash
npm ci
# Needs GITHUB_TOKEN or GH_TOKEN in the environment for the GitHub plugin:
npx semantic-release --dry-run
```

## Related docs

- [Development](development.md) — conventional commit workflow for contributors
- [Deployment](deployment.md) — pinning GHCR images on Pi / servers
- [SECURITY.md](../SECURITY.md) — supported versions track the latest tag
