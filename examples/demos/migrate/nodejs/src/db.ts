import * as dotenv from "dotenv";
dotenv.config();

const organizations = [{ id: "org1" }, { id: "org2" }, { id: "org3" }];

const subscriptions = [
  {
    organizationId: "org1",
    url: process.env.REAL_TEST_ENDPOINT || "https://org1.test/users",
    topics: ["user.created", "user.updated"],
    secret: "some_secret_value",
  },
  {
    organizationId: "org1",
    url: "https://org1.test/products",
    topics: ["product.created", "product.updated"],
    secret: "some_secret_value",
  },
  {
    organizationId: "org2",
    url: "https://org2.test/sms",
    topics: ["status.failed", "status.delivered"],
    secret: "some_secret_value",
  },
];

class Database {
  getOrganizations() {
    return organizations;
  }

  getSubscriptions(organizationId?: string) {
    if (!organizationId) {
      return subscriptions;
    }
    return subscriptions.filter(
      (subscription) => subscription.organizationId === organizationId
    );
  }
}

export default new Database();
