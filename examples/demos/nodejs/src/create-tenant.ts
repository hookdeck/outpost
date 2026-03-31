import db from "./lib/db";
import outpost from "./lib/outpost";

const tenantId = process.argv[2];
if (!tenantId) {
  console.error("Please provide an tenant ID");
  process.exit(1);
}

const main = async () => {
  const tenant = await outpost.tenants.upsert(tenantId);
  console.log(`Tenant ${tenantId} created`);
  console.log(tenant);

  const portal = await outpost.tenants.getPortalUrl(tenantId);
  console.log(`Portal URL for ${tenantId}:`, portal.redirectUrl ?? "");
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
