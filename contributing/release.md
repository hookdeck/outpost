# Release process

This document is the **primary** guide for cutting an Outpost release. It covers both the Outpost binary/Docker release and SDK generation. For SDK-specific detail (manual generation, testing, publishing), see [SDKs](sdks.md).

## Overview

An Outpost release is tag-first: you create and push a version tag (e.g. `v0.13.2`). That triggers:

1. **Outpost build and release** — the existing [release workflow](../.github/workflows/release.yml) builds the binary and Docker image and publishes them.
2. **SDK generation** — the [SDK generate on release tag](../.github/workflows/sdk-generate-on-release.yml) workflow generates all three SDKs (Go, Python, TypeScript) and opens pull requests with the changes.

After the workflows complete, you create the GitHub Release from the tag (or confirm it was created automatically).

## Process

### 1. Create and push a version tag

From the branch you want to release (typically `main`):

```bash
git tag v0.13.2   # or your next version
git push origin v0.13.2
```

Use [Semantic Versioning](https://semver.org/): `vMAJOR.MINOR.PATCH`.

### 2. On tag push — what runs

- **[release.yml](../.github/workflows/release.yml)** — Builds the Outpost binary and Docker image and publishes them (GoReleaser, Docker, etc.).
- **[sdk-generate-on-release.yml](../.github/workflows/sdk-generate-on-release.yml)** — Generates the Go, Python, and TypeScript SDKs in sequence and opens **three pull requests** (one per SDK). The workflow runs the three generations sequentially to avoid conflicts on the shared `.speakeasy/workflow.lock` (see [SDKs – SDK generation and lock files](sdks.md#sdk-generation-and-lock-files)).

### 3. SDK generation

- The tag-triggered workflow generates all three SDKs and opens PRs with the generated changes.
- **SDK versions** are determined by **Speakeasy detection** (breaking vs non-breaking changes), not by copying the Outpost tag version. Each SDK uses `versioningStrategy: automatic` in its `gen.yaml`.
- **Special case — Outpost v1.0.0:** When you cut Outpost **v1.0.0**, the workflow sets all three SDKs to version **1.0.0** so they graduate with the Outpost release.
- You will see **three SDK PRs** per release tag. Review and merge them (order does not matter; see [sdks.md](sdks.md)).

### 4. Create the GitHub Release

After the workflows complete and (if applicable) SDK PRs are merged:

1. Go to **Releases** in the repository.
2. Click **Draft a new release**.
3. Choose the **existing tag** you pushed (e.g. `v0.13.2`).
4. Add release notes and publish.

If a future workflow is added to create the Release automatically, this step may be optional.

---

## When cutting an Outpost release (checklist)

1. **Create and push the version tag** (e.g. `v0.13.2`) from the correct branch.
2. **Wait for workflows** — [release.yml](../.github/workflows/release.yml) (Outpost build) and [sdk-generate-on-release.yml](../.github/workflows/sdk-generate-on-release.yml) (SDK generation) run automatically.
3. **Review and merge the SDK PRs** — Three PRs (Go, Python, TypeScript) will be opened; merge them after review. See [SDKs](sdks.md) for testing and review guidance.
4. **Create the GitHub Release** — Draft a new release from the tag, add notes, and publish.

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
