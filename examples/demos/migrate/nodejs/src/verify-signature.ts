import * as crypto from "crypto";

function verifyWebhookSignature(
  requestBody: string,
  signatureHeader: string,
  secret: string
): boolean {
  // Parse the signature header (assumed format: t=<timestamp>,v0=<signature>)
  const parts = signatureHeader.split(",");
  const timestampPart = parts.find((part) => part.startsWith("t="));
  const signaturePart = parts.find((part) => part.startsWith("v0="));

  console.log(`signaturePart: ${signaturePart}`);
  console.log(`timestampPart: ${timestampPart}`);

  if (!timestampPart || !signaturePart) {
    return false; // Malformed signature header
  }

  const timestamp = timestampPart.split("=")[1];
  const expectedSignature = signaturePart.split("=")[1];

  // Create the string to sign
  const data = `${timestamp}.${requestBody}`;

  // Compute the HMAC SHA-256 signature
  const hmac = crypto.createHmac("sha256", secret);
  hmac.update(data);
  const computedSignature = hmac.digest("hex");

  // Compare the computed signature with the expected one
  console.log(`comparing: \n${computedSignature}\n${expectedSignature}`);

  return crypto.timingSafeEqual(
    Buffer.from(computedSignature, "utf8"),
    Buffer.from(expectedSignature, "utf8")
  );
}

// Example usage
const requestBody = '{"test":"data"}'; // The actual webhook payload
const signatureHeader =
  "t=1741797142,v0=ec25087a0b05b76fd057f61af808778b2b0e3b4c9f0dfc80f4cdc5cecdd1f325";
const secret = "some_secret_value";

const isValid = verifyWebhookSignature(requestBody, signatureHeader, secret);
console.log(`Signature valid: ${isValid}`);

// Helper function to assert valid signature
function assertValidSignature(
  secret: string,
  rawBody: Uint8Array,
  signatureHeader: string
) {
  // Parse "t={timestamp},v0={signature1,signature2}" format
  const parts = signatureHeader.split(",", 2); // Split only on first comma

  const timestampStr = parts[0].replace("t=", "");
  const signatures = parts[1].replace("v0=", "").split(",");

  const timestamp = parseInt(timestampStr, 10);
  if (isNaN(timestamp)) {
    throw new Error("timestamp should be a valid integer");
  }

  // Reconstruct the signed content
  const signedContent = `${timestamp}.${rawBody}`;

  // Generate HMAC-SHA256
  const hmac = crypto.createHmac("sha256", secret);
  hmac.update(signedContent);
  const expectedSignature = hmac.digest("hex");

  // Check if any of the signatures match
  const found = signatures.some((sig) => sig === expectedSignature);
  return found;
}

const assertResult = assertValidSignature(
  secret,
  Buffer.from(requestBody),
  signatureHeader
);
console.log(`Signature valid: ${assertResult}`);
