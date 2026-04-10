# Outpost Documentation Agent Guide

This guide defines how AI agents should contribute to documentation in this folder.

## Context

- Docs content is authored in this repository.
- Rendering is done by a private Astro application that loads docs from `docs/content`.
- Treat `docs/content`, `docs/content/nav.json`, and `docs/content/redirects.json` as the source of truth for structure, navigation, and redirects.

## Directory Map

- `content/`: Documentation pages (`.mdoc`), `nav.json`, and `redirects.json`.
- `apis/`: OpenAPI specification for API reference content.
- `public/`: Static docs assets (images, icons, etc.) when present.

## Documentation Goals

Documentation should be:

- Comprehensive across Outpost concepts, features, destinations, and operations.
- Technically accurate and implementation-aligned.
- Clear for full-stack/backend developers, engineering managers, and system administrators.
- Useful for both managed and self-hosted users.

## Writing Rules

1. Prefer host-agnostic guidance by default.
2. When behavior differs, split content with Markdoc tabs.
3. Do not use legacy `{% managed_only %}` / `{% oss_only %}` tags. Always use tabs.
4. Put environment variables in **Self-Hosted** sections. Managed sections should describe dashboard/Config API configuration.
5. If managed and self-hosted share config keys, list the keys explicitly in Managed content (do not say "same variables as self-hosted").
6. When referencing managed configuration, include the exact dashboard page deep link (not generic "dashboard" wording).
7. If the correct managed settings URL is unknown, ask for the exact link instead of guessing.
8. Keep examples realistic and copy/paste friendly.
9. Link related pages, sections, and API references where relevant.
10. Add or update `content/nav.json` when adding, moving, or renaming pages.
11. Preserve frontmatter (`title`, `description`) on all content pages.

## Available Template Variables

When writing API examples, use these Markdoc variables:

- `OUTPOST_API_ROOT`: API host root (no versioned API path). Use this when the root domain is needed.
- `OUTPOST_API_BASE_URL`: Versioned API base URL. Use this for endpoint examples such as tenant or publish routes.

Use this pattern for managed vs self-hosted divergence:

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

## Available Markdoc Components

The docs renderer supports the following Markdoc components.

### `button`

- Block tag: `{% button %}...{% /button %}`
- Attributes:
  - `text: String`
  - `href: String`
  - `target: String`
  - `type: String`
  - `width: String`

### `mockbutton`

- Self-closing tag: `{% mockbutton text="..." icon="..." /%}`
- Attributes:
  - `text: String`
  - `icon: String`

### `ref`

- Self-closing tag: `{% ref entity="..." /%}`
- Attributes:
  - `entity: String`

### `link`

- Block tag: `{% link %}...{% /link %}`
- Attributes:
  - `href: String`
  - `base: String`
  - `path: String`

### `linkCard`

- Self-closing tag: `{% linkCard ... /%}`
- Attributes:
  - `title: String`
  - `anchor: String`
  - `subtitle: String`
  - `slug: String`
  - `collection: String`

### `codeBlock`

- Block tag: `{% codeBlock %}...{% /codeBlock %}`
- Attributes:
  - `lang: String`
  - `heading: String`
  - `maxHeight: Number`

### `copyableCommand`

- Self-closing tag: `{% copyableCommand command="..." /%}`
- Attributes:
  - `command: String` (required)
  - `event: String`
  - `source: String`

### `wistiaVideo`

- Block tag: `{% wistiaVideo %}...{% /wistiaVideo %}`
- Attributes:
  - `videoId: String`
  - `title: String`

### `video`

- Block tag: `{% video %}...{% /video %}`
- Attributes:
  - `url: String`
  - `title: String`

### `callout`

- Block tag: `{% callout %}...{% /callout %}`
- Attributes:
  - `size: String`

### `comparisonTable`

- Block tag: `{% comparisonTable %}...{% /comparisonTable %}`
- Attributes:
  - `hookdeck: Number`

### `mermaid`

- Block tag: `{% mermaid %}...{% /mermaid %}`
- Attributes:
  - `content: String`

## Contribution Workflow

When creating or updating docs:

1. Edit or add files under `docs/content/...`.
2. Ensure frontmatter is present and accurate.
3. Add/update the page entry in `docs/content/nav.json` if user-visible navigation changes.
4. Verify internal links and referenced API paths.
5. Validate code snippets for syntax and practical correctness.
6. If a page is renamed/removed, add a mapping in `docs/content/redirects.json` and remove stale links.
7. If a page is renamed/removed, call out redirect requirements for the private renderer repo when relevant.

## Review Checklist

When reviewing documentation changes, verify:

- Coverage: Does it close the stated docs gap?
- Accuracy: Does it match current Outpost behavior and terminology?
- Hosting scope: Are managed and self-hosted paths both handled where needed?
- Config clarity: Are managed config paths linked to the correct dashboard page, and are self-hosted env vars documented separately?
- Clarity: Is the writing concise, scannable, and free of ambiguity?
- Links: Do links point to the right pages and sections?
- Examples: Are samples valid and consistent with current APIs/features?
- Navigation: Is `content/nav.json` updated correctly?
- Lifecycle: Are redirects captured in `content/redirects.json` and implications noted for renamed/removed pages?

If requirements are unclear or content appears incomplete, ask for clarification instead of guessing.
