import db from "./lib/db";
import outpost from "./lib/outpost";

const main = async () => {
  const organizations = db.getOrganizations();

  for (const org of organizations) {
    const portal = await outpost.tenants.getPortalUrl(org.id);
    console.log(`Portal URL for ${org.id}:`, portal.redirectUrl ?? "");
  }

  try {
    const portal = await outpost.tenants.getPortalUrl("test-tenant");
    console.log(`Portal URL for test-tenant:`, portal.redirectUrl ?? "");
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
