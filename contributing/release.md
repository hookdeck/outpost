# Release process

This document is the **primary** guide for cutting an Outpost release. It covers both the Outpost binary/Docker release and SDK generation. For SDK-specific detail (manual generation, testing, publishing), see [SDKs](sdks.md).

## Overview

**Step one: create the GitHub Release** (with the version tag). That is when Outpost is released. Following that, the tag triggers automatic SDK generation and creation of PRs for all three SDKs, **sequentially**. You then merge those PRs into main, and the SDKs are released. The tag stays on the commit you tagged; we do not move or rewrite the tag.

**Order of operations:**

1. **You:** Create the **GitHub Release** with the version tag (e.g. `v0.13.2`). That is when Outpost is released. (Create the tag from the target branch when drafting the release if it doesn’t exist yet, or push the tag first and select it.)
2. **Automated:** The tag triggers two workflows — [release.yml](../.github/workflows/release.yml) builds Outpost binaries and Docker images; [sdk-generate-on-release-dispatch.yml](../.github/workflows/sdk-generate-on-release-dispatch.yml) dispatches [sdk-generate-on-release.yml](../.github/workflows/sdk-generate-on-release.yml) (on `main`) so SDK generation runs with the correct PR base, generating the Go, Python, and TypeScript SDKs **sequentially** and opening a PR for each, targeting your default branch (e.g. `main`).
3. **You:** Merge the three SDK PRs into main.
4. **Automated:** The SDKs are released when those PRs are merged.

## Process

### 1. Create the GitHub Release (Outpost is released)

**You** create the release in GitHub; that is when Outpost is released.

1. Go to **Releases** → **Draft a new release**.
2. Attach the version tag (e.g. `v0.13.2`). If the tag doesn’t exist yet, create it from the target branch (e.g. `main`) when drafting the release, or create and push the tag first:
   ```bash
   git tag v0.13.2
   git push origin v0.13.2
   ```
   Use [Semantic Versioning](https://semver.org/): `vMAJOR.MINOR.PATCH`.
3. Add release notes and publish.

The release is tied to that tag; GitHub associates the built assets (binaries, Docker) with the release once the tag exists and [release.yml](../.github/workflows/release.yml) has run.

### 2. What runs automatically (SDK generation and PRs)

Both workflows are triggered on **tag push** (`push: tags: v*`). When you publish the GitHub Release, you attach (or create) the version tag; creating the tag in the UI causes GitHub to fire that tag push, so the workflows run right after the release is published. No change to Actions is needed: listening for the tag is equivalent for this process.

The tag triggers two workflows (they do not depend on each other):

| Workflow | What it does |
|----------|----------------|
| [release.yml](../.github/workflows/release.yml) | Builds Outpost binaries and Docker images (via GoReleaser) and uploads binary assets so they are available for the tag. Pushes Docker images to Docker Hub (e.g. `hookdeck/outpost:{{ tag }}-amd64`). |
| [sdk-generate-on-release-dispatch.yml](../.github/workflows/sdk-generate-on-release-dispatch.yml) → [sdk-generate-on-release.yml](../.github/workflows/sdk-generate-on-release.yml) | The dispatch workflow starts generation on `main` (so Speakeasy opens PRs against `main`). Auto-generates the Go, Python, and TypeScript SDKs **sequentially** and opens **three pull requests** (one per SDK), targeting your default branch. Sequential runs avoid conflicts on the shared `.speakeasy/workflow.lock` (see [SDKs – SDK generation and lock files](sdks.md#sdk-generation-and-lock-files)). |

### 3. Merge the SDK PRs into main

Review and merge the **three pull requests** (Go, Python, TypeScript) opened by the SDK generate workflows. Merge order does not matter. See [sdks.md](sdks.md) for testing and review.

- **SDK versions** are set by **Speakeasy detection** (breaking vs non-breaking), not by the Outpost tag. Exception: when you release **Outpost v1.0.0**, the workflow sets all three SDKs to **1.0.0**.

### 4. SDKs are released

When the SDK PRs are merged into main, the SDKs are released (published).

### 5. Outpost binaries (when and where)

- **When are they built?** — As soon as the tag is pushed. [release.yml](../.github/workflows/release.yml) runs and uses **GoReleaser** to build the binaries (e.g. `outpost`, `outpost-server`, `outpost-migrate-redis` for linux/amd64 and arm64), archive them (tar.gz), and build Docker images.
- **Where do they go?** — Binary archives are uploaded so they are available for that tag (e.g. as release assets once you create the release for the tag). Docker images are pushed to Docker Hub (e.g. `hookdeck/outpost:{{ tag }}-amd64`).

---

## When cutting an Outpost release (checklist)

1. **Create the GitHub Release** — In GitHub, draft a new release, attach the version tag (e.g. `v0.13.2`; create the tag from the target branch if needed), add release notes, and publish. **Outpost is released** when you publish the release.
2. **Workflows run automatically** — The tag triggers [release.yml](../.github/workflows/release.yml) (Outpost binaries + Docker) and the SDK generate workflows ([sdk-generate-on-release-dispatch.yml](../.github/workflows/sdk-generate-on-release-dispatch.yml) → [sdk-generate-on-release.yml](../.github/workflows/sdk-generate-on-release.yml)), which generates the three SDKs **sequentially** and opens a PR for each.
3. **Merge the SDK PRs** — Review and merge the three PRs (Go, Python, TypeScript) into main. See [SDKs](sdks.md) for testing and review guidance.
4. **SDKs are released** when those PRs are merged to main.

For more detail on SDK generation, versioning, and lock files, see [contributing/sdks.md](sdks.md).

## Testing the tag-triggered SDK workflow

To verify that the SDK release workflows ([sdk-generate-on-release-dispatch.yml](../.github/workflows/sdk-generate-on-release-dispatch.yml) / [sdk-generate-on-release.yml](../.github/workflows/sdk-generate-on-release.yml)) work without cutting a real release:

1. **Option A — Push a temporary tag**  
   From the branch you want to test (e.g. `main` or a feature branch):
   ```bash
   git tag v0.0.0-sdk-gen-test   # or e.g. v99.99.99-test
   git push origin v0.0.0-sdk-gen-test
   ```
   - In the repo **Actions** tab, you should see **SDK generate on release tag — dispatch** (quick) then **SDK generate on release tag** with all three jobs (generate-go, generate-python, generate-ts) in order and PRs opened against `main`.
   - To remove the tag after testing (optional):
     ```bash
     git tag -d v0.0.0-sdk-gen-test
     git push origin --delete v0.0.0-sdk-gen-test
     ```
   **Note:** Pushing a tag also triggers [release.yml](../.github/workflows/release.yml) (Outpost build/release). If you use a test tag, consider using a clearly non-release value (e.g. `v0.0.0-sdk-gen-test`) and skip creating a GitHub Release for it.

2. **Option B — Manual run from Actions**  
   Go to **Actions → SDK generate on release tag** (the Speakeasy run, not the dispatch workflow), click **Run workflow**, choose the branch (e.g. `main`), optionally set **release_tag** (e.g. `v0.0.0-test`), and run. This does not push a tag; useful for a quick smoke test. Note: the workflow sets all SDKs to `1.0.0` only when **release_tag** is exactly `v1.0.0`; otherwise Speakeasy detection applies.
