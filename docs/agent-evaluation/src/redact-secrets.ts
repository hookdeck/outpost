/**
 * Best-effort secret redaction for eval artifacts (transcripts, judge failures, CI uploads).
 * Not a security guarantee — treat artifacts as sensitive even after redaction.
 */

const ENV_SECRET_KEYS = ["OUTPOST_API_KEY", "ANTHROPIC_API_KEY"] as const;

export function collectEnvSecretValues(): string[] {
  const out: string[] = [];
  for (const key of ENV_SECRET_KEYS) {
    const value = process.env[key]?.trim();
    if (value && value.length >= 8) {
      out.push(value);
    }
  }
  return out;
}

/** Pattern-based redaction (headers, env lines, query params). Optional maxLen for UI previews. */
export function redactSecrets(text: string, maxLen?: number): string {
  let s = text;
  s = s.replace(
    /\bAuthorization:\s*Bearer\s+\S+/gi,
    "Authorization: Bearer [REDACTED]",
  );
  s = s.replace(
    /\bAuthorization:\s*Basic\s+[A-Za-z0-9+/=]+/gi,
    "Authorization: Basic [REDACTED]",
  );
  s = s.replace(/Bearer\s+sk-ant-api[^\s"'`<>]+/gi, "Bearer [REDACTED]");
  s = s.replace(/Bearer\s+sk-proj-[^\s"'`<>]+/gi, "Bearer [REDACTED]");
  s = s.replace(/Bearer\s+[A-Za-z0-9._~-]{20,}/g, "Bearer [REDACTED]");
  s = s.replace(/\bx-api-key\s*:\s*[^\s\n]+/gi, "x-api-key: [REDACTED]");
  s = s.replace(/\bapi-key\s*:\s*[^\s\n]+/gi, "api-key: [REDACTED]");
  s = s.replace(/\bx-auth-token\s*:\s*[^\s\n]+/gi, "x-auth-token: [REDACTED]");
  s = s.replace(/\baccess-token\s*:\s*[^\s\n]+/gi, "access-token: [REDACTED]");
  s = s.replace(
    /([?&](?:api[_-]?key|access[_-]?token|token|client[_-]?secret|secret)=)([^&#\s"'`<>]+)/gi,
    "$1[REDACTED]",
  );
  s = s.replace(
    /\b([A-Z][A-Z0-9_]*(?:KEY|TOKEN|SECRET|PASSWORD))=([^\s\n"'`#]+)/g,
    "$1=[REDACTED]",
  );
  if (maxLen !== undefined && s.length > maxLen) {
    s = `${s.slice(0, maxLen)}…`;
  }
  return s;
}

function redactKnownLiteralValues(text: string, secrets: readonly string[]): string {
  let s = text;
  for (const secret of secrets) {
    if (secret.length >= 8) {
      s = s.split(secret).join("[REDACTED]");
    }
  }
  return s;
}

/** Full artifact redaction: patterns plus literal values from the current process env. */
export function redactSecretsForArtifact(text: string): string {
  return redactKnownLiteralValues(redactSecrets(text), collectEnvSecretValues());
}

export function redactEvalArtifactJson(value: unknown): string {
  return `${redactSecretsForArtifact(JSON.stringify(value, null, 2))}\n`;
}
