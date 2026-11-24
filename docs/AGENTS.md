# Outpost Documentation Agent Guide

How to contribute to the docs in this folder now lives as a repo-local **Agent Skill**:
[`.agents/skills/hookdeck-outpost-docs-format/SKILL.md`](../.agents/skills/hookdeck-outpost-docs-format/SKILL.md).

It covers everything that used to be in this file — directory map, frontmatter, the
managed-vs-self-hosted tab pattern, template variables, Markdoc components, navigation and
redirects, the contribution workflow and review checklist — plus the voice rules Outpost
docs follow. The skill is self-contained: no external dependencies.

## Loading the skill

- **Cursor, Codex, and other agents** load `.agents/skills/` automatically — nothing to do.
- **Claude Code:** from the repo root, run `npx skills add . --skill hookdeck-outpost-docs-format`
  and choose **symlink** at the prompt (see [vercel-labs/skills](https://github.com/vercel-labs/skills)).

## Context

- Docs content is authored in this repository under `docs/content`.
- Rendering is done by a separate (private) Astro application that loads docs from `docs/content`.
- Treat `docs/content`, `docs/content/nav.json`, and `docs/content/redirects.json` as the
  source of truth for structure, navigation, and redirects.
