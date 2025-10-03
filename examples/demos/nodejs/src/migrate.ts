import * as process from "process";
import { askQuestion } from "./lib/utils";

// Outpost API wrapper
import outpost from "./lib/outpost";

// Database wrapper
import { default as db } from "./lib/db";

const cleanup = async () => {
  const answer = await askQuestion(
    "Do you wish to clean up existing data? Y/N and then 'Enter' to confirm: "
  );
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
      secret: subscription.secret,
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
    console.error("Migration failed:", error.message);

    askQuestion("Press 'e' and Enter to see the full error details: ").then(
      (answer) => {
        if (answer === "e") {
          console.error(error);
        }
        process.exit(1);
      }
    );
  });
