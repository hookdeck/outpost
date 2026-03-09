# Release process

This document is the **primary** guide for cutting an Outpost release. It covers both the Outpost binary/Docker release and SDK generation. For SDK-specific detail (manual generation, testing, publishing), see [SDKs](sdks.md).

## Overview

The release flow is: **tag to generate assets** → **merge the SDK PRs into main** → **create the release in GitHub**. We merge the SDK PRs before creating the release so the SDKs are in main when we publish. The tag stays on the commit you tagged; we do not move or rewrite the tag.

**Order of operations:**

1. **You:** Create and push a version tag (e.g. `v0.13.2`). This triggers the workflows that build assets and open the three SDK PRs (generated from that tag).
2. **Automated:** Two workflows run — one builds Outpost binaries and Docker images, the other generates SDKs and opens PRs targeting your default branch (e.g. `main`).
3. **You:** Review and merge the three SDK PRs into main.
4. **You:** Create the GitHub Release (draft a new release, choose that tag, add notes, publish).

## Process

### 1. Create and push a version tag

From the branch you want to release (typically `main`):

```bash
git tag v0.13.2   # or your next version
git push origin v0.13.2
```

Use [Semantic Versioning](https://semver.org/): `vMAJOR.MINOR.PATCH`.

### 2. What runs automatically on tag push (asset generation)

When you push a tag, two workflows run (they do not depend on each other):

| Workflow | What it does |
|----------|----------------|
| [release.yml](../.github/workflows/release.yml) | Builds Outpost binaries and Docker images (via GoReleaser) and uploads binary assets so they are available for the tag. Pushes Docker images to Docker Hub (e.g. `hookdeck/outpost:{{ tag }}-amd64`). |
| [sdk-generate-on-release.yml](../.github/workflows/sdk-generate-on-release.yml) | Generates the Go, Python, and TypeScript SDKs in sequence and opens **three pull requests** (one per SDK). Runs sequentially to avoid conflicts on the shared `.speakeasy/workflow.lock` (see [SDKs – SDK generation and lock files](sdks.md#sdk-generation-and-lock-files)). |

### 3. Merge the SDK PRs (before creating the release)

The [sdk-generate-on-release.yml](../.github/workflows/sdk-generate-on-release.yml) workflow opens **three pull requests** (Go, Python, TypeScript), generated from the tag you pushed. Merge them into main **before** you create the GitHub Release so the SDKs are in main when we publish the release.

- **SDK versions** are set by **Speakeasy detection** (breaking vs non-breaking), not by the Outpost tag. Exception: when you release **Outpost v1.0.0**, the workflow sets all three SDKs to **1.0.0**.
- Review and merge the three SDK PRs (order does not matter). See [sdks.md](sdks.md) for testing and review.

### 4. Create the GitHub Release

**You** create the release in GitHub after the workflows have run and the SDK PRs are merged:

1. Go to **Releases** → **Draft a new release**.
2. Choose the **existing tag** (e.g. `v0.13.2`). The release is tied to that tag (the tag stays on the commit you originally tagged), so GitHub associates the built assets (binaries, etc.) with this release.
3. Add release notes and publish.

Order: **tag → workflows generate assets and open SDK PRs → merge SDK PRs → create the release in GitHub**.

### 5. Outpost binaries (when and where)

- **When are they built?** — As soon as the tag is pushed. [release.yml](../.github/workflows/release.yml) runs and uses **GoReleaser** to build the binaries (e.g. `outpost`, `outpost-server`, `outpost-migrate-redis` for linux/amd64 and arm64), archive them (tar.gz), and build Docker images.
- **Where do they go?** — Binary archives are uploaded so they are available for that tag (e.g. as release assets once you create the release for the tag). Docker images are pushed to Docker Hub (e.g. `hookdeck/outpost:{{ tag }}-amd64`).

---

## When cutting an Outpost release (checklist)

1. **Create and push the version tag** (e.g. `v0.13.2`) from the correct branch. This triggers asset generation and opens the three SDK PRs.
2. **Wait for workflows** — [release.yml](../.github/workflows/release.yml) (Outpost binaries + Docker) and [sdk-generate-on-release.yml](../.github/workflows/sdk-generate-on-release.yml) (SDK PRs) run automatically.
3. **Merge the SDK PRs** — Review and merge the three PRs (Go, Python, TypeScript) into main. See [SDKs](sdks.md) for testing and review guidance.
4. **Create the GitHub Release** — In GitHub, draft a new release, choose the tag (it stays on the commit you tagged), add release notes, and publish. The built assets are associated with the release via that tag.

For more detail on SDK generation, versioning, and lock files, see [contributing/sdks.md](sdks.md).

## Testing the tag-triggered SDK workflow

To verify that [sdk-generate-on-release.yml](../.github/workflows/sdk-generate-on-release.yml) works without cutting a real release:

1. **Option A — Push a temporary tag**  
   From the branch you want to test (e.g. `main` or a feature branch):
   ```bash
   git tag v0.0.0-sdk-gen-test   # or e.g. v99.99.99-test
   git push origin v0.0.0-sdk-gen-test
   ```
   - In the repo **Actions** tab, open the run for **SDK generate on release tag** and confirm all three jobs (generate-go, generate-python, generate-ts) run in order and open PRs (or commit, depending on the Speakeasy action behaviour).
   - To remove the tag after testing (optional):
     ```bash
     git tag -d v0.0.0-sdk-gen-test
     git push origin --delete v0.0.0-sdk-gen-test
     ```
   **Note:** Pushing a tag also triggers [release.yml](../.github/workflows/release.yml) (Outpost build/release). If you use a test tag, consider using a clearly non-release value (e.g. `v0.0.0-sdk-gen-test`) and skip creating a GitHub Release for it.

2. **Option B — Manual run from Actions**  
   The workflow supports **Run workflow** from the Actions tab (workflow_dispatch). Go to **Actions → SDK generate on release tag**, click **Run workflow**, choose the branch (e.g. `main`), and run. This uses the branch ref instead of a tag, so no tag or release is created; useful for a quick smoke test. Note: `set_version` is only applied when the ref is tag `v1.0.0`, so on manual runs Speakeasy detection applies.
