import db from "./lib/db";
import outpost from "./lib/outpost";

// Node.js arguments: [0] = node, [1] = script path, [2] = first user argument
const tenantId = process.argv[2];
if (!tenantId) {
  console.error("Please provide an tenant ID");
  process.exit(1);
}

const main = async () => {
  const response = await outpost.registerTenant(tenantId);
  console.log(`Tenant ${tenantId} created`);
  console.log(response);

  const portalUrl = await outpost.getPortalURL(tenantId);
  console.log(`Portal URL for ${tenantId}:`, portalUrl);
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
