import * as process from "process";

import outpost from "./lib/outpost";
import db from "./lib/db";

const main = async () => {
  const organizations = db.getOrganizations();

  for (const organization of organizations) {
    const destinations = await outpost.destinations.list(organization.id);
    for (const destination of destinations) {
      const topics = destination.topics;
      const topic =
        Array.isArray(topics) && topics.length > 0
          ? topics[0]
          : "test.event";
      const data = {
        test: "data",
        from_organization_id: organization.id,
        from_destination_id: destination.id,
        timestamp: new Date().toISOString(),
      };
      const event = {
        tenantId: organization.id,
        topic,
        eligibleForRetry: true,
        data,
        metadata: {
          some: "metadata",
        },
      };
      console.log("Publishing event");
      console.log(event);
      await outpost.publish.event(event);
    }
  }
};

main()
  .then(() => {
    console.log("Test publishing complete");
    process.exit(0);
  })
  .catch((err) => {
    console.error("Test publishing failed", err);
    process.exit(1);
  });
