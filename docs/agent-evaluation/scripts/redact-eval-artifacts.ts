#!/usr/bin/env -S node --import tsx
/**
 * Best-effort in-place redaction of JSON under results/runs/ before CI artifact upload.
 * See src/redact-secrets.ts.
 */

import { readdir, readFile, stat, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { redactSecretsForArtifact } from "../src/redact-secrets.js";

const EVAL_ROOT = join(fileURLToPath(new URL(".", import.meta.url)), "..");
const RUNS_DIR = join(EVAL_ROOT, "results", "runs");

async function walkJsonFiles(dir: string): Promise<string[]> {
  const entries = await readdir(dir, { withFileTypes: true });
  const files: string[] = [];
  for (const entry of entries) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      files.push(...(await walkJsonFiles(path)));
    } else if (entry.isFile() && entry.name.endsWith(".json")) {
      files.push(path);
    }
  }
  return files;
}

async function main(): Promise<void> {
  try {
    await stat(RUNS_DIR);
  } catch {
    console.error("redact-eval-artifacts: no results/runs directory — nothing to do");
    return;
  }

  const files = await walkJsonFiles(RUNS_DIR);
  let updated = 0;
  for (const path of files) {
    const raw = await readFile(path, "utf8");
    const redacted = redactSecretsForArtifact(raw);
    if (redacted !== raw) {
      await writeFile(path, redacted.endsWith("\n") ? redacted : `${redacted}\n`, "utf8");
      updated++;
    }
  }
  console.error(
    `redact-eval-artifacts: scanned ${files.length} JSON file(s), redacted ${updated}`,
  );
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
