import * as process from "process";

import outpost from "./outpost";
import db from "./db";

const main = async () => {
  const subscriptions = db.getSubscriptions();
  const testSubscription = subscriptions.find(
    (subscription) => subscription.url === process.env.REAL_TEST_ENDPOINT
  );

  const destinations = await outpost.getDestinations(
    subscriptions[0].organizationId
  );

  console.log("Destinations:", destinations);

  if (testSubscription) {
    console.log("Found test subscription:", testSubscription);
    await outpost.publishEvent({
      tenant_id: testSubscription.organizationId,
      topic: testSubscription.topics[0],
      data: { test: "data" },
      meta_data: {
        some: "metadata",
      },
    });
  } else {
    console.log("Test subscription not found.");
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
