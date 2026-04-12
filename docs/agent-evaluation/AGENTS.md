# Agent evaluation — authoring rules for humans & coding agents

This file applies to **everything under `docs/agent-evaluation/`** (scenarios, README, tracker, harness TypeScript). Follow it when adding or editing eval specs so we do not **teach to the test** or confuse **evaluator docs** with **in-character user speech**.

## Who reads what

| Audience                 | Content                                                                                                                                                                                   |
| ------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **The model under test** | Turn 0 = pasted [`hookdeck-outpost-agent-prompt.mdoc`](../agent-evaluation/hookdeck-outpost-agent-prompt.md) template only, plus **Turn N — User** blockquotes (verbatim user role-play). |
| **Humans / harness**     | Intent, preconditions, eval harness JSON, Success criteria, Failure modes, `score-transcript.ts`, README.                                                                                 |

**Never** put harness vocabulary into **user** lines. The user is a product engineer, not an eval runner.

## Anti-leakage rules (user turns)

In **`### Turn N — User`** blockquotes, **do not** use:

- **Option 1 / 2 / 3** (those labels exist only inside the dashboard template; a real user says what they want in plain language).
- **Turn 0**, **Turn 1**, or any **turn** numbering (that is script metadata).
- Phrases like **“the instructions you already have”**, **“the full-stack section of the prompt”**, **“follow the Hookdeck Outpost template”** as a stand-in for requirements (the model already has Turn 0; state the _product ask_, not a pointer to a doc section).
- **“Match the prompt”**, **“dashboard prompt”**, **“eval”**, **“scenario”**, **“success criteria”**, **heuristic names**, **`scoreScenarioNN`**.

**Do** use natural operator language: stack, repo, product behavior, security (key on server), domain topics, README/env, Hookdeck project/topics **as the customer would say them**.

It is fine for **Success criteria**, **Failure modes**, and **Intent** to name `scoreScenarioNN`, Turn 0, Option 3, etc. — those sections are not pasted as the user.

## Alignment without parroting

- **Product bar** (domain publish, topic reconciliation, full-stack UI depth) belongs in **Success criteria** and in the **prompt template** in `hookdeck-outpost-agent-prompt.mdoc`.
- **User turns** should **request outcomes** (“I need customers to see failed deliveries and retry”) not **cite** where in the template that is spelled out.

If you add a new requirement, update **Success criteria** (and heuristics only when a **durable, low–false-positive** check exists). Do not stuff the verbatim rubric into the user quote.

## Pre-merge checklist (scenarios)

Before merging changes to `scenarios/*.md`:

- [ ] Every **`> ...` user** line reads like a **real customer** message (read aloud test).
- [ ] No **Option N** / **Turn 0** / **scenario** / **prompt section** leakage in user blockquotes.
- [ ] **Success criteria** still state the full bar; nothing removed from criteria and only moved into user text.
- [ ] If integration depth changed, **`src/score-transcript.ts`** and this **README** scenario table are updated when rubrics change.

## Where Cursor loads this

- A **repo-root** [`AGENTS.md`](../../AGENTS.md) points here so agents see this folder’s rules.
- [`.cursor/rules/agent-evaluation-authoring.mdc`](../../.cursor/rules/agent-evaluation-authoring.mdc) applies when editing paths under `docs/agent-evaluation/`.
