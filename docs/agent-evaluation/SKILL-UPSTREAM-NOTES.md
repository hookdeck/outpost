# Notes for updating `hookdeck/agent-skills` — `skills/outpost`

Apply these in the **[agent-skills](https://github.com/hookdeck/agent-skills)** repository, not in Outpost OSS.

## Recommended direction

1. **Lead with managed Hookdeck Outpost** — Link prominently to managed quickstarts (curl, TypeScript, Python, Go) and `https://api.outpost.hookdeck.com/2025-07-01`.
2. **Fix REST examples** — Tenant upsert must be `PUT {base}/tenants/{tenant_id}`, not `PUT {base}/{tenant_id}`.
3. **Align env naming** — Match product/docs: Outpost API key from project **Settings → Secrets**, typically loaded as `OUTPOST_API_KEY` in examples; avoid introducing `HOOKDECK_API_KEY` unless the dashboard literally uses that name.
4. **Self-hosted section** — Keep Docker/Kubernetes/Railway as a secondary path with `http://localhost:3333/api/v1` and correct `/tenants/...` paths.
5. **Optional: split later** — If the file grows, add `outpost-managed.md` / `outpost-self-hosted.md` fragments or separate skills; keep the default tile entrypoint short.

## Concrete issues in current `SKILL.md` (as of fetch against `main`)

- **Wrong curl path:** `curl -X PUT "$BASE_URL/$TENANT_ID"` should target `/tenants/$TENANT_ID` relative to the API base (managed base has no `/api/v1` prefix).
- **Managed auth row** — Verify exact dashboard copy for secret name and env var conventions; link to Hookdeck Outpost project settings, not only generic dashboard secrets if URLs differ.
- **Tile summary** — `tile.json` says “self-hosted relay”; managed Outpost should be reflected in the summary string when GA positioning is final.

## Cross-links from this repo

- Onboarding prompt template: `docs/pages/quickstarts/hookdeck-outpost-agent-prompt.mdx`
- Manual agent eval harness: `docs/agent-evaluation/README.md`