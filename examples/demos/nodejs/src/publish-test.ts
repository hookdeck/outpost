import * as process from "process";

import outpost from "./lib/outpost";
import db from "./lib/db";

const main = async () => {
  const organizations = db.getOrganizations();

  for (const organization of organizations) {
    const destinations = await outpost.getDestinations(organization.id);
    for (const destination of destinations) {
      const event = {
        tenant_id: organization.id,
        topic: destination.topics[0],
        data: {
          test: "data",
          from_organization_id: organization.id,
          from_destination_id: destination.id,
          timestamp: new Date().toISOString(),
        },
        metadata: {
          some: "metadata",
        },
      };
      console.log("Publishing event");
      console.log(event);
      await outpost.publishEvent(event);
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
