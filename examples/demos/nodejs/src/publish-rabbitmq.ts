import * as process from "process";
import * as amqp from "amqplib";
import * as dotenv from "dotenv";
import db from "./lib/db";

dotenv.config();

// Note: Exchange empty string refers to the default exchange which often has publishing restrictions
const RABBITMQ_URL =
  process.env.RABBITMQ_URL || "amqp://guest:guest@localhost:5673";
const EXCHANGE_NAME = process.env.RABBITMQ_EXCHANGE || "outpost"; // Set a default exchange name
const ROUTING_KEY = process.env.RABBITMQ_ROUTING_KEY || "";
const QUEUE_NAME = process.env.RABBITMQ_QUEUE || "publish";

const main = async () => {
  const org = db.getOrganizations()[0];
  const sub = db.getSubscriptions(org.id)[0];

  console.log(`Connecting to RabbitMQ at ${RABBITMQ_URL}`);

  try {
    const connection = await amqp.connect(RABBITMQ_URL);
    const channel = await connection.createChannel();

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
        source: "rabbitmq-test",
        priority: "high",
        test: "true",
      },
      timestamp: Date.now(),
    };

    const content = Buffer.from(JSON.stringify(message));

    const options = {
      contentType: "application/json",
      headers: {
        "x-source": "outpost-example",
        "x-test": "true",
      },
      messageId: message.id,
      timestamp: Math.floor(Date.now() / 1000),
    };

    console.log(
      `Publishing message to exchange ${EXCHANGE_NAME} with routing key ${ROUTING_KEY}`
    );
    console.log(JSON.stringify(message, null, 2));

    const result = channel.publish(
      EXCHANGE_NAME,
      ROUTING_KEY,
      content,
      options
    );
    console.log(`Message published successfully: ${result}`);

    const checkQueue = await channel.checkQueue(QUEUE_NAME);
    console.log(`Queue ${QUEUE_NAME} has ${checkQueue.messageCount} messages`);

    setTimeout(() => {
      connection.close();
      console.log("Connection closed");
    }, 500);
  } catch (error) {
    console.error("Error publishing message to RabbitMQ:", error);
    process.exit(1);
  }
};

main()
  .then(() => {
    console.log("RabbitMQ publishing complete");
    process.exit(0);
  })
  .catch((err) => {
    console.error("RabbitMQ publishing failed", err);
    process.exit(1);
  });
