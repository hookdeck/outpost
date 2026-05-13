import { randomUUID } from "crypto";
import dotenv from "dotenv";
dotenv.config();
import { Outpost } from "@hookdeck/outpost-sdk";

const ADMIN_API_KEY = process.env.ADMIN_API_KEY;
const TENANT_ID = process.env.TENANT_ID;
const apiServerURL =
  process.env.API_BASE_URL ||
  `${process.env.SERVER_URL || "http://localhost:3333"}/api/v1`;

if (!ADMIN_API_KEY) {
  console.error("Please set the ADMIN_API_KEY environment variable.");
  process.exit(1);
}
if (!TENANT_ID) {
  console.error("Please set the TENANT_ID environment variable.");
  process.exit(1);
}
const tenantId = TENANT_ID as string;

const debugLogger = {
  debug: (message: string) => {
    console.log("DEBUG", message);
  },
  group: (message: string) => {
    console.group(message);
  },
  groupEnd: () => {
    console.groupEnd();
  },
  log: (message: string) => {
    console.log(message);
  }
};

// 0.13.1: use the tenant-scoped API key (from tenants.getToken). List destinations only returns destinations for that tenant.
const withTenantApiKey = async (tenantApiKey: string) => {
  const outpost = new Outpost({ apiKey: tenantApiKey, serverURL: apiServerURL });
  const destinations = await outpost.destinations.list(tenantId);

  console.log(destinations);
}

const withAdminApiKey = async () => {
  const outpost = new Outpost({ apiKey: ADMIN_API_KEY, serverURL: apiServerURL });

  try {
    const result = await outpost.health.check();
    console.log(result);
  } catch (err: unknown) {
    const msg = err && typeof err === "object" && "statusCode" in err && (err as { statusCode: number }).statusCode === 404
      ? "Health endpoint not available (e.g. managed Outpost). Skipping."
      : String(err);
    console.log(msg);
  }

  const topic = `user.created`;
  const newDestinationName = `My Test Destination ${randomUUID()}`;

  console.log(`Creating tenant: ${tenantId}`);
  const tenant = await outpost.tenants.upsert(tenantId);
  console.log("Tenant created successfully:", tenant);

  console.log(
    `Creating destination: ${newDestinationName} for tenant ${tenantId}...`
  );
  const destination = await outpost.destinations.create(tenantId, {
    type: "webhook",
    config: {
      url: "https://example.com/webhook-receiver",
    },
    topics: ["user.created"],
  });
  console.log("Destination created successfully:", destination);

  const eventPayload = {
    userId: "user_456",
    orderId: "order_xyz",
    timestamp: new Date().toISOString(),
  };

  console.log(
    `Publishing event to topic ${topic} for tenant ${tenantId}...`
  );
  await outpost.publish({
    data: eventPayload,
    tenantId,
    topic: "user.created",
    eligibleForRetry: true,
  });

  console.log("Event published successfully");

  const destinations = await outpost.destinations.list(tenantId);

  console.log(destinations);

  // List tenants (v0.14+: tenants.list(request) with a single request object)
  const tenantsPage = await outpost.tenants.list({ limit: 5 });
  console.log("Tenants (first page):", tenantsPage.result?.models ?? []);

  // Get portal URL (Admin API Key required)
  try {
    const portal = await outpost.tenants.getPortalUrl(tenantId);
    console.log("Portal URL:", portal.redirectUrl ?? portal);
  } catch (err: unknown) {
    const msg = err && typeof err === "object" && "statusCode" in err && (err as { statusCode: number }).statusCode === 404
      ? "Portal endpoint not available (e.g. disabled or managed Outpost). Skipping."
      : String(err);
    console.log("Get portal URL:", msg);
  }

  const tokenRes = await outpost.tenants.getToken(tenantId);
  console.log("Token response:", tokenRes);

  await withTenantApiKey(tokenRes.token!);
}

const main = async () => {
  await withAdminApiKey();
}

main().catch((e: unknown) => {
  const err = e as { statusCode?: number; message?: string };
  if (err?.statusCode === 401 || (typeof err?.message === "string" && err.message.includes("Unauthorized"))) {
    console.error(
      "List destinations with tenant token returned 401. The server could not verify the JWT — ensure API_JWT_SECRET is set on the Outpost deployment (see sdks/schemas/README.md).",
      e
    );
  } else {
    console.error(e);
  }
  process.exit(1);
});
