import inquirer from 'inquirer';
import dotenv from "dotenv";
dotenv.config();
import { Outpost } from "@hookdeck/outpost-sdk";
import { CreateTenantDestinationRequest } from '@hookdeck/outpost-sdk/dist/esm/models/operations';

const ADMIN_API_KEY = process.env.ADMIN_API_KEY;
const TENANT_ID = process.env.TENANT_ID || "hookdeck";
const SERVER_URL = process.env.OUTPOST_URL || "http://localhost:3333";

if (!ADMIN_API_KEY) {
  console.error("Please set the ADMIN_API_KEY environment variable.");
  process.exit(1);
}

async function main() {
  const outpostAdmin = new Outpost({
    security: { adminApiKey: ADMIN_API_KEY },
    serverURL: `${SERVER_URL}/api/v1`,
  });

  await outpostAdmin.tenants.upsert({
    tenantId: TENANT_ID,
  });

  const { destinationType } = await inquirer.prompt([
    {
      type: 'list',
      name: 'destinationType',
      message: 'Select a destination type:',
      choices: ['azure_servicebus'],
    },
  ]);

  let destinationCreateRequest: CreateTenantDestinationRequest | null = null;

  if (destinationType === 'azure_servicebus') {
    console.log(`
You can create a topic or queue specific connection string with send-only permissions using the Azure CLI.
Please replace $RESOURCE_GROUP, $NAMESPACE_NAME, and $TOPIC_NAME with your actual values.

Create a send-only policy for the topic:
\`\`\`bash
az servicebus topic authorization-rule create \\
  --resource-group $RESOURCE_GROUP \\
  --namespace-name $NAMESPACE_NAME \\
  --topic-name $TOPIC_NAME \\
  --name SendOnlyPolicy \\
  --rights Send
\`\`\`

or for a queue:

\`\`\`bash
az servicebus queue authorization-rule create \\
  --resource-group $RESOURCE_GROUP \\
  --namespace-name $NAMESPACE_NAME \\
  --queue-name $QUEUE_NAME \\
  --name SendOnlyPolicy \\
  --rights Send
\`\`\`

Get the Topic-Specific Connection String:
\`\`\`bash
az servicebus topic authorization-rule keys list \\
  --resource-group $RESOURCE_GROUP \\
  --namespace-name $NAMESPACE_NAME \\
  --topic-name $TOPIC_NAME \\
  --name SendOnlyPolicy \\
  --query primaryConnectionString \\
  --output tsv
\`\`\`

or for a Queue-Specific Connection String:
\`\`\`bash
az servicebus queue authorization-rule keys list \\
  --resource-group $RESOURCE_GROUP \\
  --namespace-name $NAMESPACE_NAME \\
  --queue-name $QUEUE_NAME \\
  --name SendOnlyPolicy \\
  --query primaryConnectionString \\
  --output tsv
\`\`\`

`);
    const { connectionString, topic_or_queue_name } = await inquirer.prompt([
        {
            type: 'input',
            name: 'connectionString',
            message: 'Enter Azure Service Bus Connection String:',
        },
        {
            type: 'input',
            name: 'topic_or_queue_name',
            message: 'Enter Azure Service Bus Topic or Queue name:',
        }
    ]);
    destinationCreateRequest = {
      tenantId: TENANT_ID,
      destinationCreate: {
        credentials: {
          connectionString,
        },
        type: "azure_servicebus",
        config: {
          name: topic_or_queue_name
        },
        topics: "*",
      }
    }

  } else {
    console.log(`Destination type "${destinationType}" is not supported by this script.`);
    return;
  }

  try {
    const destination = await outpostAdmin.destinations.create(destinationCreateRequest);
    console.log("Destination created successfully:", destination);
  } catch (error) {
    console.error("An error occurred:", error);
  }
}

main();