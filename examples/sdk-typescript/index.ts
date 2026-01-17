import { randomUUID } from "crypto";
import dotenv from "dotenv";
dotenv.config();
import { Outpost } from "@hookdeck/outpost-sdk";

const ADMIN_API_KEY = process.env.ADMIN_API_KEY;
const SERVER_URL = process.env.OUTPOST_URL || "http://localhost:3333";

if (!ADMIN_API_KEY) {
  console.error("Please set the ADMIN_API_KEY environment variable.");
  process.exit(1);
}

async function manageOutpostResources() {
  // 1. Create an Outpost instance using the AdminAPIKey
  const outpostAdmin = new Outpost({
    security: { adminApiKey: ADMIN_API_KEY },
    serverURL: `${SERVER_URL}/api/v1`,
  });

  const tenantId = `hookdeck`;
  const topic = `user.created`;
  const newDestinationName = `My Test Destination ${randomUUID()}`;

  try {
    // 2. Create a tenant
    console.log(`Creating tenant: ${tenantId}`);
    const tenant = await outpostAdmin.tenants.upsert({
      tenantId,
    });
    console.log("Tenant created successfully:", tenant);

    // 3. Create a destination for the tenant
    console.log(
      `Creating destination: ${newDestinationName} for tenant ${tenantId}...`
    );
    const destination = await outpostAdmin.destinations.create({
      tenantId,
      params: {
        type: "webhook",
        config: {
          url: "https://example.com/webhook-receiver",
        },
        topics: [topic],
      },
    });
    console.log("Destination created successfully:", destination);

    // 4. Publish an event for the created tenant
    const eventPayload = {
      userId: "user_456",
      orderId: "order_xyz",
      timestamp: new Date().toISOString(),
    };

    console.log(`Publishing event to topic ${topic} for tenant ${tenantId}...`);
    await outpostAdmin.publish.event({
      data: eventPayload,
      tenantId,
      topic,
      eligibleForRetry: true,
    });

    console.log("Event published successfully");
  } catch (error) {
    console.error("An error occurred:", error);
  }
}

manageOutpostResources();