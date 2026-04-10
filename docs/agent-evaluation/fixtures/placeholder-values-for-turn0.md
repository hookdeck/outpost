# Placeholder values for Turn 0 (eval / local testing)

The **prompt template itself** lives in one place only:

`**[hookdeck-outpost-agent-prompt.mdx](../../pages/quickstarts/hookdeck-outpost-agent-prompt.mdx)`** (from repo root: `docs/pages/quickstarts/...`) — copy the fenced block under **## Template**, then replace each `{{PLACEHOLDER}}` using the table below.

Do **not** paste real API keys into chat. Have operators put `OUTPOST_API_KEY` in a project `**.env`** (or another loader), not in the agent transcript. Use a throwaway Hookdeck project when possible.

For `**npm run eval -- --scenario …**` (or `**--scenarios**` / `**--all**`), the runner only needs `**ANTHROPIC_API_KEY**` and `**EVAL_TEST_DESTINATION_URL**`. To score a **full** eval (generated commands/code actually work), you still need `**OUTPOST_API_KEY`** (and usually `**OUTPOST_TEST_WEBHOOK_URL**`) when you **execute** the agent’s output afterward. Optional `**EVAL_LOCAL_DOCS=1`** points Turn 0 at repo paths instead of live `{{DOCS_URL}}` links.

---

## Example substitutions (non-secret)


| Placeholder                | Example                                                                                                                                                             |
| -------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `{{API_BASE_URL}}`         | `https://api.outpost.hookdeck.com/2025-07-01`                                                                                                                       |
| `{{TOPICS_LIST}}`          | `- user.created`                                                                                                                                                    |
| `{{TEST_DESTINATION_URL}}` | Hookdeck Console **Source** URL the dashboard feeds in (for automated evals, set `EVAL_TEST_DESTINATION_URL` to the same value). Example: `https://hkdk.events/...` |
| `{{DOCS_URL}}`             | `https://outpost.hookdeck.com/docs` (local Zudoku: same paths under `/docs`)                                                                                        |
| `{{LLMS_FULL_URL}}`        | Omit the line in the template if unused, or your public `llms-full.txt` URL                                                                                         |


---

## Dashboard implementation note

When this text is embedded in the Hookdeck product, the **same** template body should be rendered from one dashboard/backend source so docs and product stay aligned. The MDX page in this repo is the documentation **canonical** copy until product source is wired to match it.