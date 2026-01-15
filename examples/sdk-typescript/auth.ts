import { randomUUID } from "crypto";
import dotenv from "dotenv";
dotenv.config();
import { Outpost } from "@hookdeck/outpost-sdk";

const TENANT_ID = process.env.TENANT_ID;
const ADMIN_API_KEY = process.env.ADMIN_API_KEY;
const SERVER_URL = process.env.SERVER_URL;

if (!process.env.ADMIN_API_KEY) {
  console.error("Please set the ADMIN_API_KEY environment variable.");
  process.exit(1);
}
if (!process.env.SERVER_URL) {
  console.error("Please set the SERVER_URL environment variable.");
  process.exit(1);
}
if (!process.env.TENANT_ID ) {
  console.error("Please set the TENANT_ID environment variable.");
  process.exit(1);
}

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

const withJwt  = async (jwt: string) => {
  const outpost = new Outpost({security: {tenantJwt: jwt}, serverURL: `${SERVER_URL}/api/v1`, tenantId: TENANT_ID });
  const destinations = await outpost.destinations.list({});

  console.log(destinations);
}

const withAdminApiKey  = async () => {
  const outpost = new Outpost({security: {adminApiKey: ADMIN_API_KEY}, serverURL: `${SERVER_URL}/api/v1`});

  const result = await outpost.health.check();
  console.log(result);

  const tenantId = `hookdeck`;
  const topic = `user.created`;
  const newDestinationName = `My Test Destination ${randomUUID()}`;

  console.log(`Creating tenant: ${tenantId}`);
  const tenant = await outpost.tenants.upsert({
    tenantId: tenantId,
  });
  console.log("Tenant created successfully:", tenant);

  console.log(
    `Creating destination: ${newDestinationName} for tenant ${tenantId}...`
  );
  const destination = await outpost.destinations.create({
    tenantId,
    params: {
      type: "webhook",
      config: {
        url: "https://example.com/webhook-receiver",
      },
      topics: ["user.created"],
    }
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
  await outpost.publish.event({
    data: eventPayload,
    tenantId,
    topic: "user.created",
    eligibleForRetry: true,
  });

  console.log("Event published successfully");

  const destinations = await outpost.destinations.list({tenantId: TENANT_ID})

  console.log(destinations);

  const jwt = await outpost.tenants.getToken({tenantId: TENANT_ID});

  await withJwt(jwt.token!);
}

const main = async () => {
  await withAdminApiKey();
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
