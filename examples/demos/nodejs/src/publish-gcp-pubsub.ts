import * as process from "process";
import * as dotenv from "dotenv";
import { PubSub } from "@google-cloud/pubsub";
import db from "./lib/db";

dotenv.config();

function validateEnvVars() {
  const requiredEnvVars = [
    { name: "GCP_PROJECT_ID", value: process.env.GCP_PROJECT_ID },
    { name: "GCP_PUBSUB_TOPIC_ID", value: process.env.GCP_PUBSUB_TOPIC_ID },
    {
      name: "GOOGLE_APPLICATION_CREDENTIALS",
      value: process.env.GOOGLE_APPLICATION_CREDENTIALS,
    },
  ];

  const missingVars = requiredEnvVars
    .filter((v) => !v.value)
    .map((v) => v.name);

  if (missingVars.length > 0) {
    console.error(
      `Error: Missing required environment variables: ${missingVars.join(", ")}`
    );
    console.error("Ensure GCP_PROJECT_ID and GCP_PUBSUB_TOPIC_ID are set.");
    console.error(
      "Authentication requires GOOGLE_APPLICATION_CREDENTIALS environment variable set to the path of your service account key file, or running in a GCP environment with Application Default Credentials (ADC)."
    );
    process.exit(1);
  }
}

validateEnvVars();

const GCP_PROJECT_ID = process.env.GCP_PROJECT_ID!;
const GCP_PUBSUB_TOPIC_ID = process.env.GCP_PUBSUB_TOPIC_ID!;

const main = async () => {
  const org = db.getOrganizations()[0];
  const sub = db.getSubscriptions(org.id)[0];

  console.log(`Connecting to Google Cloud Pub/Sub project ${GCP_PROJECT_ID}`);

  try {
    // Create Pub/Sub client
    // Authentication is handled automatically via ADC or GOOGLE_APPLICATION_CREDENTIALS env var
    const pubSubClient = new PubSub({ projectId: GCP_PROJECT_ID });

    const topic = pubSubClient.topic(GCP_PUBSUB_TOPIC_ID);

    const messagePayload = {
      id: `msg-${Date.now()}`,
      topic: sub.topics[0], // This might need adjustment depending on how topics are mapped
      tenant_id: org.id,
      eligible_for_retry: true,
      data: {
        user_id: `user-${Math.floor(Math.random() * 10000)}`,
        name: "Test User",
        email: "test@example.com",
        created_at: new Date().toISOString(),
      },
      metadata: {
        source: "gcp-pubsub-publish-test", // Updated source
        priority: "high",
        test: "true",
      },
      timestamp: Date.now(),
    };

    const messageBody = JSON.stringify(messagePayload);
    const dataBuffer = Buffer.from(messageBody);

    const attributes = {
      "x-source": "outpost-example",
      "x-test": "true",
      // Add other relevant attributes if needed
    };

    console.log(
      `Publishing message to GCP Pub/Sub topic: ${GCP_PUBSUB_TOPIC_ID}`
    );
    console.log(JSON.stringify(messagePayload, null, 2));

    // Publish message to Pub/Sub
    const messageId = await topic.publishMessage({
      data: dataBuffer,
      attributes,
    });

    console.log(`Message published successfully with ID: ${messageId}`);
  } catch (error) {
    console.error("Error publishing message to GCP Pub/Sub:", error);
    process.exit(1);
  }
};

main()
  .then(() => {
    console.log("GCP Pub/Sub publishing complete");
    process.exit(0);
  })
  .catch((err) => {
    console.error("GCP Pub/Sub publishing failed", err);
    process.exit(1);
  });
