/**
 * Unit-style assertions for secret redaction (no test runner dependency).
 *
 *   npm run test:redact-secrets
 */

import {
  collectEnvSecretValues,
  redactEvalArtifactJson,
  redactSecrets,
  redactSecretsForArtifact,
} from "./redact-secrets.js";

function assert(cond: boolean, msg: string): void {
  if (!cond) {
    throw new Error(`Assertion failed: ${msg}`);
  }
}

function assertNotIncludes(haystack: string, needle: string, msg: string): void {
  if (haystack.includes(needle)) {
    throw new Error(`Assertion failed: ${msg} (found "${needle}")`);
  }
}

function withEnv(
  values: Record<string, string | undefined>,
  fn: () => void,
): void {
  const keys = Object.keys(values);
  const prior = new Map<string, string | undefined>();
  for (const key of keys) {
    prior.set(key, process.env[key]);
    const value = values[key];
    if (value === undefined) {
      delete process.env[key];
    } else {
      process.env[key] = value;
    }
  }
  try {
    fn();
  } finally {
    for (const key of keys) {
      const value = prior.get(key);
      if (value === undefined) {
        delete process.env[key];
      } else {
        process.env[key] = value;
      }
    }
  }
}

function testRedactSecretsPatterns(): void {
  const fake_bearer_token = "faketokenabcdefghijklmnopqrstuvw";
  const bearer =
    `curl -H "Authorization: Bearer ${fake_bearer_token}" https://api.example.com`;
  const redacted_bearer = redactSecrets(bearer);
  assertNotIncludes(redacted_bearer, fake_bearer_token, "Bearer token redacted");
  assert(redacted_bearer.includes("[REDACTED]"), "Bearer placeholder present");

  const env_line = "OUTPOST_API_KEY=opst_live_secret_value_12345678";
  const redacted_env = redactSecrets(env_line);
  assertNotIncludes(redacted_env, "opst_live_secret", "env KEY= line redacted");
  assert(redacted_env.includes("OUTPOST_API_KEY=[REDACTED]"), "env key name preserved");

  const query = "https://example.com/hook?api_key=supersecretvalue&topic=user.created";
  const redacted_query = redactSecrets(query);
  assertNotIncludes(redacted_query, "supersecretvalue", "query api_key redacted");
  assert(redacted_query.includes("api_key=[REDACTED]"), "query param name preserved");

  const long = "x".repeat(50);
  const truncated = redactSecrets(long, 10);
  assert(truncated.length === 11, "maxLen adds ellipsis char");
  assert(truncated.endsWith("…"), "maxLen suffix");
  assert(truncated.startsWith("x".repeat(10)), "maxLen prefix preserved");
}

function testCollectEnvSecretValues(): void {
  withEnv(
    {
      OUTPOST_API_KEY: "short",
      ANTHROPIC_API_KEY: undefined,
    },
    () => {
      assert(
        collectEnvSecretValues().length === 0,
        "secrets shorter than 8 chars are ignored",
      );
    },
  );

  withEnv(
    {
      OUTPOST_API_KEY: "opst_test_key_abcdefghij",
      ANTHROPIC_API_KEY: "anthropic_fake_key_abcdefghijklmnop",
    },
    () => {
      const values = collectEnvSecretValues();
      assert(values.length === 2, "collects both env secrets when long enough");
      assert(
        values.includes("opst_test_key_abcdefghij"),
        "includes OUTPOST_API_KEY value",
      );
    },
  );
}

function testWebhookUrlLiteralRedaction(): void {
  const fake_webhook_url = "https://events.example.test/webhook/fake_ci_destination_path_01";
  withEnv(
    {
      EVAL_TEST_DESTINATION_URL: fake_webhook_url,
      OUTPOST_TEST_WEBHOOK_URL: fake_webhook_url,
      OUTPOST_API_KEY: undefined,
      ANTHROPIC_API_KEY: undefined,
    },
    () => {
      assert(collectEnvSecretValues().length === 1, "dedupes identical webhook URL env values");
      const raw = `Turn 0 prompt includes test destination: ${fake_webhook_url}`;
      const redacted = redactSecretsForArtifact(raw);
      assertNotIncludes(redacted, fake_webhook_url, "webhook URL literal redacted");
      assert(redacted.includes("[REDACTED]"), "webhook placeholder present");
    },
  );
}

function testRedactSecretsForArtifact(): void {
  withEnv(
    {
      OUTPOST_API_KEY: "opst_literal_echo_12345678",
      ANTHROPIC_API_KEY: undefined,
    },
    () => {
      const raw =
        "Agent echoed the key verbatim: opst_literal_echo_12345678 in tool output";
      const redacted = redactSecretsForArtifact(raw);
      assertNotIncludes(redacted, "opst_literal_echo_12345678", "literal env value redacted");
      assert(redacted.includes("[REDACTED]"), "literal placeholder present");
    },
  );
}

function testRedactEvalArtifactJson(): void {
  withEnv(
    {
      OUTPOST_API_KEY: "opst_json_embed_123456789",
      ANTHROPIC_API_KEY: undefined,
    },
    () => {
      const payload = {
        meta: { scenarioId: "02" },
        messages: [
          {
            role: "assistant",
            content: "export OUTPOST_API_KEY=opst_json_embed_123456789",
          },
        ],
      };
      const out = redactEvalArtifactJson(payload);
      assert(out.endsWith("\n"), "artifact JSON ends with newline");
      assertNotIncludes(out, "opst_json_embed_123456789", "JSON artifact redacts secrets");
      assert(out.includes('"scenarioId": "02"'), "non-secret JSON preserved");
      JSON.parse(out.trim());
    },
  );
}

function main(): void {
  testRedactSecretsPatterns();
  testCollectEnvSecretValues();
  testWebhookUrlLiteralRedaction();
  testRedactSecretsForArtifact();
  testRedactEvalArtifactJson();
  console.error("redact-secrets.test: OK");
}

main();
