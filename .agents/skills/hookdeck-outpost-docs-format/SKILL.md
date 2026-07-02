---
name: hookdeck-outpost-docs-format
description: "How to write and structure Outpost documentation in this repo — where pages live (docs/content), frontmatter shape, the managed-vs-self-hosted tab pattern, template variables, Markdoc components, navigation and redirects, plus the voice rules Outpost docs follow. Use when creating, editing, or reviewing docs under docs/content. Self-contained: this skill owns both structure and voice for Outpost docs, with no external dependency."
---

# Writing Outpost docs

This skill owns how Outpost documentation is written and structured. Outpost is open source (Apache-2.0), so this skill is **self-contained** — it embeds the voice rules directly rather than depending on any other skill, so anyone editing these docs has everything they need in-repo.

Docs content is authored in this repository under `docs/content` and rendered by a separate (private) Astro application. Treat `docs/content`, `docs/content/nav.json`, and `docs/content/redirects.json` as the source of truth for structure, navigation, and redirects.

## Where docs live

- `docs/content/` — documentation pages (`.mdoc`), plus `nav.json` and `redirects.json`.
- `docs/apis/` — OpenAPI specification for API reference content.
- `docs/public/` — static docs assets (images, icons) when present.

## Audience and goals

Outpost docs serve **platform / SaaS operators** and the engineers who run them: full-stack and backend developers, engineering managers, and system administrators — across **both managed and self-hosted** deployments. Docs should be:

- Comprehensive across Outpost concepts, features, destinations, and operations.
- Technically accurate and implementation-aligned.
- Clear and useful for both managed and self-hosted users.

## Voice

Outpost docs are published as official Hookdeck documentation at **hookdeck.com/docs/outpost**, so they must read in the same voice as the rest of the Hookdeck docs. The rules below mirror Hookdeck's canonical docs style guide (see "Voice source of truth" at the end of this section) — an Outpost page should be indistinguishable in tone from any other hookdeck.com/docs page.

The voice is an engineer explaining the system to another engineer: confident and specific, never marketing.

### Strong rules

1. **Direct address — "you", not "the user".** Speak to the reader. Avoid "the user", "users", "the developer", "the customer" (third-person abstractions). "We" / "our" is rare — only for design intent ("we recommend the first approach"). Define Outpost terms on first use — **tenant**, **destination**, **topic**, **event**, **delivery attempt** — then use them consistently.
2. **Definition-first openings.** Open a concept page with what the concept *is*, before examples or background. Pattern: `<concept> + is | lets you | allows you to…`. Follow with one or two short paragraphs of context or trade-off framing.
3. **Short paragraphs** — roughly one to two sentences each (~1–3 sentences max). Break a paragraph that covers more than one idea.
4. **Descriptive link text** that names what's at the destination — `[Event Destinations](https://eventdestinations.org)`, not "click here", "this page", "learn more", or "see here". If a sentence forces "click here", rewrite it so the link text names the target.
5. **Honest about trade-offs.** When two valid approaches exist, name both, state the recommendation, and give a sentence on what the alternative buys. Be transparent about what's beta vs. stable, and about managed-vs-self-hosted differences — over-selling early-stage behavior loses trust fastest.
6. **No marketing hype.** Avoid "powerful", "seamless", "effortless", "best-in-class", "world-class", "enterprise-grade", "robust" (about Outpost itself), "blazingly fast", "comprehensive", "next-generation", "leverage" (as a verb), "easily" / "simply" (when implying something is trivial), "out-of-the-box" (as marketing). Say what a thing *does*, not how good it is. Borderline words are fine when they name a real technical property ("a robust retry strategy" the reader is building). Be **specific over generic**: name protocols, standards, destinations (AWS SQS, Kafka, GCP Pub/Sub, RabbitMQ), version numbers, and exact config keys.
7. **American English** — behavior not behaviour, optimize not optimise, center not centre.

### When editing existing pages (match surrounding text)

- **Concept capitalization.** New page: lowercase concept nouns (`tenant`, `destination`, `topic`, `event`). Editing a page: match its existing convention. Correcting: don't change capitalization unless that's what's being corrected.
- **Emdashes and horizontal rules.** Don't *add* new emdashes (prefer commas, periods, or "and"/"but") or new horizontal rules in prose — but don't *strip* existing ones as part of a content edit. Removing them is style normalization, out of scope for a content change.
- **Title capitalization.** Default to sentence case for new page titles; match the surrounding pattern when editing.

### Voice source of truth

These rules are the public, Outpost-scoped mirror of Hookdeck's canonical docs style guide, which Hookdeck maintains internally. They are embedded directly in this repo — rather than installed as a shared dependency — because Outpost is public and open source. **If the canonical style guide and this section ever diverge, the canonical guide wins; update this section to match.** When Hookdeck docs voice changes, update this section too.

## Frontmatter

Outpost pages use a **minimal** frontmatter: `title` and `description` only. No `id`, `icon`, or `llms_description` (those belong to the hookdeck.com/website docs, which are a different system).

```yaml
---
title: "Concepts"
description: "Core concepts and architecture of Outpost: tenants, destinations, topics, events, and delivery attempts."
---
```

- `title` — a noun phrase naming the page ("Concepts", "Overview", "SDKs").
- `description` — one sentence describing what the page is about.
- **Preserve frontmatter** (`title`, `description`) on every content page.

## Managed vs. Self-Hosted

This is the core Outpost writing pattern. **Prefer host-agnostic guidance by default.** When behavior genuinely differs between managed and self-hosted, split the content with Markdoc tabs:

```md
{% tabs %}
{% tab title="Managed" %}
Managed-specific instructions.
{% /tab %}
{% tab title="Self-Hosted" %}
Self-hosted-specific instructions.
{% /tab %}
{% /tabs %}
```

Rules for splitting:

- **Environment variables go in Self-Hosted sections.** Managed sections describe dashboard / Config API configuration instead.
- When referencing managed configuration, include the **exact dashboard page deep link** — not a generic "in the dashboard". If you don't know the correct settings URL, ask for it rather than guessing.
- If managed and self-hosted **share config keys**, list the keys explicitly in the Managed content — don't write "same variables as self-hosted".
- Keep examples realistic and copy/paste friendly.

## Template variables

Use these Markdoc variables in API examples so hosts and base paths stay correct:

- `OUTPOST_API_ROOT` — API host root (no versioned API path). Use when the root domain is needed.
- `OUTPOST_API_BASE_URL` — versioned API base URL. Use for endpoint examples such as tenant or publish routes.

## Markdoc components

The docs renderer supports these components:

| Component | Tag | Key attributes |
|---|---|---|
| `button` | `{% button %}…{% /button %}` | `text`, `href`, `target`, `type`, `width` |
| `mockbutton` | `{% mockbutton text="…" icon="…" /%}` | `text`, `icon` |
| `ref` | `{% ref entity="…" /%}` | `entity` |
| `link` | `{% link %}…{% /link %}` | `href`, `base`, `path` |
| `linkCard` | `{% linkCard … /%}` | `title`, `anchor`, `subtitle`, `slug`, `collection` |
| `codeBlock` | `{% codeBlock %}…{% /codeBlock %}` | `lang`, `heading`, `maxHeight` |
| `copyableCommand` | `{% copyableCommand command="…" /%}` | `command` (required), `event`, `source` |
| `wistiaVideo` | `{% wistiaVideo %}…{% /wistiaVideo %}` | `videoId`, `title` |
| `video` | `{% video %}…{% /video %}` | `url`, `title` |
| `callout` | `{% callout %}…{% /callout %}` | `size` |
| `comparisonTable` | `{% comparisonTable %}…{% /comparisonTable %}` | `hookdeck` |
| `mermaid` | `{% mermaid %}…{% /mermaid %}` | `content` |

Don't invent components — one not in this list won't render.

## Navigation and redirects

- **`docs/content/nav.json`** is the navigation source of truth. Add or update a page's entry whenever user-visible navigation changes (adding, moving, or renaming a page).
- **`docs/content/redirects.json`** holds `{ "from": "…", "to": "…" }` mappings. When a page is renamed or removed, add a redirect mapping and remove stale links. Note redirect implications for the private renderer repo when relevant.

## Contribution workflow

1. Edit or add files under `docs/content/…`.
2. Ensure frontmatter (`title`, `description`) is present and accurate.
3. Update `docs/content/nav.json` if user-visible navigation changes.
4. Verify internal links and referenced API paths.
5. Validate code snippets for syntax and practical correctness.
6. If a page is renamed or removed, add a mapping in `docs/content/redirects.json` and remove stale links.

## Review checklist

When reviewing documentation changes, verify:

- **Coverage** — does it close the stated docs gap?
- **Accuracy** — does it match current Outpost behavior and terminology?
- **Hosting scope** — are managed and self-hosted paths both handled where needed?
- **Config clarity** — are managed config paths linked to the correct dashboard page, and are self-hosted env vars documented separately?
- **Clarity** — is the writing concise, scannable, and unambiguous?
- **Links** — do links point to the right pages and sections?
- **Examples** — are samples valid and consistent with current APIs/features?
- **Navigation** — is `nav.json` updated correctly?
- **Lifecycle** — are redirects captured in `redirects.json`, with implications noted for renamed/removed pages?

If requirements are unclear or content appears incomplete, ask for clarification instead of guessing.

## What this skill does not own

- **Rendering** — the docs are rendered by a separate private Astro application; this repo owns the content, not the renderer.
- **The rest of the hookdeck.com documentation** — the main docs are authored separately, with a different frontmatter and component set. This skill is Outpost-only.
