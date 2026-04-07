import * as crypto from "crypto";

/**
 * Verify Outpost default-mode webhooks (v0.12+ defaults).
 * Signed content: raw request body. Header value: v0=<hex>[,<hex>...] during secret rotation.
 * Timestamp for replay checks lives in x-outpost-timestamp, not in the signature header.
 */
function verifyWebhookSignature(
  rawBody: string,
  signatureHeader: string,
  secret: string
): boolean {
  const trimmed = signatureHeader.trim();
  if (!trimmed.startsWith("v0=")) {
    return false;
  }

  const listed = trimmed.slice("v0=".length).split(",").map((s) => s.trim());
  const expected = crypto
    .createHmac("sha256", secret)
    .update(rawBody)
    .digest("hex");

  const expectedBuf = Buffer.from(expected, "utf8");
  return listed.some((sig) => {
    const sigBuf = Buffer.from(sig, "utf8");
    return (
      sigBuf.length === expectedBuf.length &&
      crypto.timingSafeEqual(sigBuf, expectedBuf)
    );
  });
}

/**
 * Legacy format when the operator sets DESTINATIONS_WEBHOOK_SIGNATURE_* to v0.11-style templates.
 * Header: t=<unix>,v0=<hex>[,<hex>...]  Signed content: "<unix>.<raw body>"
 */
function verifyWebhookSignatureLegacy(
  rawBody: string,
  signatureHeader: string,
  secret: string
): boolean {
  const comma = signatureHeader.indexOf(",");
  if (comma === -1) {
    return false;
  }
  const tsPart = signatureHeader.slice(0, comma);
  const sigPart = signatureHeader.slice(comma + 1);
  if (!tsPart.startsWith("t=") || !sigPart.startsWith("v0=")) {
    return false;
  }

  const timestamp = tsPart.slice("t=".length);
  const listed = sigPart.slice("v0=".length).split(",").map((s) => s.trim());
  const signedContent = `${timestamp}.${rawBody}`;
  const expected = crypto
    .createHmac("sha256", secret)
    .update(signedContent)
    .digest("hex");

  const expectedBuf = Buffer.from(expected, "utf8");
  return listed.some((sig) => {
    const sigBuf = Buffer.from(sig, "utf8");
    return (
      sigBuf.length === expectedBuf.length &&
      crypto.timingSafeEqual(sigBuf, expectedBuf)
    );
  });
}

// --- Default (current) Outpost behavior ---
const requestBody = '{"test":"data"}';
const signatureHeader =
  "v0=5920020651a5934e394f95a7e79a85400ba12318c11f330b9ca30c7f064318d1";
const secret = "some_secret_value";

const isValidDefault = verifyWebhookSignature(
  requestBody,
  signatureHeader,
  secret
);
console.log(`Default format signature valid: ${isValidDefault}`);

// --- Legacy (optional operator override) ---
const legacyHeader =
  "t=1741797142,v0=ec25087a0b05b76fd057f61af808778b2b0e3b4c9f0dfc80f4cdc5cecdd1f325";
const isValidLegacy = verifyWebhookSignatureLegacy(
  requestBody,
  legacyHeader,
  secret
);
console.log(`Legacy format signature valid: ${isValidLegacy}`);
