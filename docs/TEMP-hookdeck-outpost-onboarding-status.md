# Hookdeck Outpost onboarding — status (temporary)

**Purpose:** Track implementation status for the managed quickstarts, agent prompt, and related work. **Delete this file** when tracking moves elsewhere (e.g. Linear, parent epic).

**Last updated:** 2026-04-07

---

## Done (Outpost OSS repo)

- Managed quickstarts: `hookdeck-outpost-curl.mdx`, `-typescript.mdx`, `-python.mdx`, `-go.mdx`
- Agent prompt template page: `hookdeck-outpost-agent-prompt.mdx`
- Zudoku sidebar: **Quickstarts → Hookdeck Outpost** (above **Self-Hosted**)
- `quickstarts.mdx` index: managed vs self-hosted links
- Content aligned with product copy: API key from **Settings → Secrets**, standard markdown (no `:::tip`), verify via Hookdeck Console + project logs
- SDK examples: env vars section, numbered quickstart scripts with step comments

## Pending / follow-up

- **QA:** Run TypeScript, Python, and Go examples against live managed API; confirm all doc links resolve on production docs URL
- **Test destination URL:** When `console.hookdeck.com` (or equivalent) has a stable public URL format, update quickstarts if it replaces “create a Console Source” instructions
- **Hookdeck Dashboard:** Two-step onboarding (topics → copy agent prompt) with placeholder injection (`{{API_BASE_URL}}`, `{{TOPICS_LIST}}`, `{{TEST_DESTINATION_URL}}`, `{{DOCS_URL}}`, optional `{{LLMS_FULL_URL}}`); env var UI for `OUTPOST_API_KEY` (not in prompt body)
- **Hookdeck Astro site:** Consume MDX, `llms.txt` / `llms-full.txt` / `.md` exports, canonical `DOCS_URL` (e.g. `https://hookdeck.com/outpost/docs`)
- **Deferred (not blocking GA):** Broader docs IA (“Self-Hosted” under Guides, redirects for moved pages) per original plan

## References

- OpenAPI / managed base URL: `https://api.outpost.hookdeck.com/2025-07-01` (in `docs/apis/openapi.yaml` `servers`)
- Agent template source: `docs/pages/quickstarts/hookdeck-outpost-agent-prompt.mdx`