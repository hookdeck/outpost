import { Outpost } from "@hookdeck/outpost-sdk";
import dotenv from "dotenv";
dotenv.config();


async function main() {
  const serverURL = process.env.SERVER_URL;
  if (!serverURL) {
    throw new Error("SERVER_URL is not set");
  }

  const apiKey = process.env.ADMIN_API_KEY;
  if (!apiKey) {
    throw new Error("ADMIN_API_KEY is not set");
  }

  const tenantId = process.env.TENANT_ID;
  if (!tenantId) {
    throw new Error("TENANT_ID is not set");
  }

  const client = new Outpost({
    serverURL: `${serverURL}/api/v1`,
    security: {
      adminApiKey: apiKey,
    }
  });

  const topic = "order.created";
  const payload = {
    order_id: "ord_2Ua9d1o2b3c4d5e6f7g8h9i0j",
    customer_id: "cus_1a2b3c4d5e6f7g8h9i0j",
    total_amount: "99.99",
    currency: "USD",
    items: [
        {
            product_id: "prod_1a2b3c4d5e6f7g8h9i0j",
            name: "Example Product 1",
            quantity: 1,
            price: "49.99",
        },
        {
            product_id: "prod_9z8y7x6w5v4u3t2s1r0q",
            name: "Example Product 2",
            quantity: 1,
            price: "50.00",
        },
    ],
  };

  try {
    const response = await client.publish.event({
      topic: topic,
      data: payload,
      tenantId: tenantId,
    });
    console.log("Event published successfully:", response);
  } catch (error) {
    console.error("Error publishing event:", error);
  }
}

main();