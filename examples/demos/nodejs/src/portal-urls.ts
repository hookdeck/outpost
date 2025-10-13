import db from "./lib/db";
import outpost from "./lib/outpost";

const main = async () => {
  const organizations = db.getOrganizations();

  for (const org of organizations) {
    const portalUrl = await outpost.getPortalURL(org.id);
    console.log(`Portal URL for ${org.id}:`, portalUrl);
  }

  try {
    const portalUrl = await outpost.getPortalURL("test-tenant");
    console.log(`Portal URL for test-tenant:`, portalUrl);
  } catch (error) {
    console.error(`Failed to create portal for test-tenant:`, error);
  }
};

main()
  .then(() => {
    console.log("Done");
    process.exit(0);
  })
  .catch((err) => {
    console.error("Error", err);
    process.exit(1);
  });
