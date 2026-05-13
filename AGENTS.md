# Coding agent notes (Outpost)

When you change files under **`docs/agent-evaluation/`** (scenarios, scoring, harness docs), read and apply **[`docs/agent-evaluation/AGENTS.md`](docs/agent-evaluation/AGENTS.md)** first. It defines anti–“teach to the test” rules for user-turn wording and scenario structure.

For this repo’s PR review format, see **`CLAUDE.md`**.

## Website (`hookdeck.com`) and `docs/content`

Product docs under **`docs/content/`** (and images under **`docs/public/images/`**) are consumed by the **[`hookdeck/website`](https://github.com/hookdeck/website)** repo at build time. When those paths change on **`main`**, a GitHub Action notifies Hookdeck, which triggers a Vercel deploy of the site. See [`.github/workflows/trigger-website-deploy.yml`](https://github.com/hookdeck/outpost/blob/main/.github/workflows/trigger-website-deploy.yml) (repo variable `HOOKDECK_WEBSITE_DEPLOY_SOURCE_URL`, secret `VERCEL_WEBSITE_DEPLOY_HOOK_SOURCE_API_KEY`).
