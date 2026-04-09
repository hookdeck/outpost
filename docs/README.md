# Outpost Documentation

This directory contains the source content for the Outpost docs.

The public docs experience is rendered by a private Astro application that loads files from this repository, so this folder is the source of truth for page content and navigation.

## How Documentation Is Structured

- `content/` contains documentation pages (mostly `.mdoc`, some `.mdx`) and the nav config.
- `content/nav.json` defines sidebar groups, page order, and visible titles.
- `apis/openapi.yaml` contains the API specification used by API reference pages.
- `public/` (when present) stores static assets (images, icons, etc.).

## Authoring Guidelines

The docs should:

- Cover both managed and self-hosted Outpost flows.
- Stay technically accurate while remaining easy to follow.
- Link to related pages when useful.
- Include runnable, realistic examples.

When managed and self-hosted behavior differs, use Markdoc tabs:

```md
{% tabs %}
{% tab title="Managed" %}
Managed-specific content.
{% /tab %}
{% tab title="Self-Hosted" %}
Self-hosted-specific content.
{% /tab %}
{% /tabs %}
```

## Adding or Updating a Page

1. Create or edit the page under `content/` (for example `content/features/my-feature.mdoc`).
2. Add or update frontmatter (`title`, `description`).
3. Add the page to the appropriate section in `content/nav.json`.
4. Verify links and examples.
5. If a page is renamed or removed, ensure redirects are handled in the site renderer repository.
