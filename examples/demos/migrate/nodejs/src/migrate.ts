import * as process from "process";
import readline from "readline";

import * as dotenv from "dotenv";
dotenv.config();

if (!process.env.OUTPOST_API_BASE_URL || !process.env.OUTPOST_API_KEY) {
  console.error("OUTPOST_API_BASE_URL and OUTPOST_API_KEY are required");
  process.exit(1);
}

const askQuestion = (query: string): Promise<string> => {
  const rl = readline.createInterface({
    input: process.stdin,
    output: process.stdout,
  });

  return new Promise((resolve) => {
    rl.question(query, (answer) => {
      rl.close();
      resolve(answer);
    });
  });
};

// Outpost API wrapper
import OutpostClient from "./outpost";

const outpost = new OutpostClient(
  process.env.OUTPOST_API_BASE_URL,
  process.env.OUTPOST_API_KEY
);

// Database wrapper
import { default as db } from "./db";

const cleanup = async () => {
  const answer = await askQuestion("Press 'Y' and then 'Enter' to confirm: ");
  if (answer !== "Y") {
    console.log("Skipping cleanup...");
    return;
  }

  console.log("Cleaning up existing data...");

  const organizations = db.getOrganizations();
  for (const organization of organizations) {
    const destinations = await outpost.getDestinations(organization.id);
    for (const destination of destinations) {
      console.log(`Deleting destination:`, destination);
      await outpost.deleteDestination(organization.id, destination.id);
    }
    await outpost.deleteTenant(organization.id);
  }
};

const listTopics = async () => {
  const subscriptions = db.getSubscriptions();
  const allTopics = new Set<string>();

  for (const subscription of subscriptions) {
    for (const topic of subscription.topics) {
      allTopics.add(topic);
    }
  }

  return Array.from(allTopics);
};

const migrateOrganizations = async () => {
  const migratedOrgIds: string[] = [];
  const organizations = db.getOrganizations();
  for (const organization of organizations) {
    await outpost.registerTenant(organization.id);
    migratedOrgIds.push(organization.id);
  }
  return migratedOrgIds;
};

const migrateSubscriptions = async (organizationId: string) => {
  const subscriptions = db.getSubscriptions(organizationId);
  for (const subscription of subscriptions) {
    await outpost.createDestination({
      tenant_id: organizationId,
      type: "webhook",
      url: subscription.url,
      topics: subscription.topics,
      signing_secret: subscription.signing_secret,
    });
  }
};

const main = async () => {
  await cleanup();

  const topics = await listTopics();
  console.log("Subscription topics:", topics);

  const migratedOrgIds = await migrateOrganizations();

  for (const organizationId of migratedOrgIds) {
    await migrateSubscriptions(organizationId);

    const portalUrl = await outpost.getPortalURL(organizationId);
    console.log(`Portal URL for ${organizationId}:`, portalUrl);
  }
};

main()
  .then(() => {
    console.log("Migration complete");

    process.exit(0);
  })
  .catch((error) => {
    askQuestion("Press 'e' and Enter to see the full error details: ").then(
      (answer) => {
        if (answer === "e") {
          console.error(error);
        }
        process.exit(1);
      }
    );
  });
