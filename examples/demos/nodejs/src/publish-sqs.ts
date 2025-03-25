import * as process from "process";
import * as dotenv from "dotenv";
import {
  SQSClient,
  SendMessageCommand,
  GetQueueAttributesCommand,
  QueueAttributeName,
} from "@aws-sdk/client-sqs";
import db from "./lib/db";

dotenv.config();

// Validate required environment variables
function validateEnvVars() {
  const requiredEnvVars = [
    { name: "SQS_QUEUE_URL", value: process.env.SQS_QUEUE_URL },
    { name: "AWS_REGION", value: process.env.AWS_REGION },
  ];

  // Using AWS SDK with explicit credentials requires both keys
  if (process.env.AWS_ACCESS_KEY_ID || process.env.AWS_SECRET_ACCESS_SECRET) {
    requiredEnvVars.push(
      { name: "AWS_ACCESS_KEY_ID", value: process.env.AWS_ACCESS_KEY_ID },
      {
        name: "AWS_SECRET_ACCESS_SECRET",
        value: process.env.AWS_SECRET_ACCESS_SECRET,
      }
    );
  }

  const missingVars = requiredEnvVars
    .filter((v) => !v.value)
    .map((v) => v.name);

  if (missingVars.length > 0) {
    console.error(
      `Error: Missing required environment variables: ${missingVars.join(", ")}`
    );
    process.exit(1);
  }
}

validateEnvVars();

const SQS_QUEUE_URL = process.env.SQS_QUEUE_URL;
const AWS_REGION = process.env.AWS_REGION;

const main = async () => {
  const org = db.getOrganizations()[0];
  const sub = db.getSubscriptions(org.id)[0];

  console.log(`Connecting to AWS SQS in region ${AWS_REGION}`);

  try {
    // Create SQS client
    const sqsClient = new SQSClient({
      region: AWS_REGION,
      // If running locally with custom credentials
      credentials:
        process.env.AWS_ACCESS_KEY_ID && process.env.AWS_SECRET_ACCESS_SECRET
          ? {
              accessKeyId: process.env.AWS_ACCESS_KEY_ID,
              secretAccessKey: process.env.AWS_SECRET_ACCESS_SECRET,
            }
          : undefined,
    });

    const message = {
      id: `msg-${Date.now()}`,
      topic: sub.topics[0],
      tenant_id: org.id,
      data: {
        user_id: `user-${Math.floor(Math.random() * 10000)}`,
        name: "Test User",
        email: "test@example.com",
        created_at: new Date().toISOString(),
      },
      metadata: {
        source: "sqs-test",
        priority: "high",
        test: "true",
      },
      timestamp: Date.now(),
    };

    const messageBody = JSON.stringify(message);

    const sendParams = {
      QueueUrl: SQS_QUEUE_URL,
      MessageBody: messageBody,
      MessageAttributes: {
        "x-source": {
          DataType: "String",
          StringValue: "outpost-example",
        },
        "x-test": {
          DataType: "String",
          StringValue: "true",
        },
      },
    };

    console.log(`Publishing message to SQS queue: ${SQS_QUEUE_URL}`);
    console.log(JSON.stringify(message, null, 2));

    // Send message to SQS
    const sendResult = await sqsClient.send(new SendMessageCommand(sendParams));
    console.log(
      `Message published successfully with ID: ${sendResult.MessageId}`
    );
    // Get queue attributes to check message count
    const queueAttributesParams = {
      QueueUrl: SQS_QUEUE_URL,
      AttributeNames: [QueueAttributeName.ApproximateNumberOfMessages],
    };

    const queueAttributes = await sqsClient.send(
      new GetQueueAttributesCommand(queueAttributesParams)
    );

    console.log(
      `Queue has approximately ${
        queueAttributes.Attributes?.ApproximateNumberOfMessages || 0
      } messages`
    );
  } catch (error) {
    console.error("Error publishing message to SQS:", error);
    process.exit(1);
  }
};

main()
  .then(() => {
    console.log("SQS publishing complete");
    process.exit(0);
  })
  .catch((err) => {
    console.error("SQS publishing failed", err);
    process.exit(1);
  });
