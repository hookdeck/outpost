# RabbitMQ Configuration Instructions

RabbitMQ is a message-broker software that implements the Advanced Message Queuing Protocol (AMQP). It acts as a middleman for various services, accepting, storing, and forwarding binary data messages. It provides features such as:

- Reliable messaging with persistence, delivery acknowledgments, and high availability
- Flexible routing through exchanges before messages arrive at queues
- Clustering capability to form a single logical broker from multiple servers
- Management UI for monitoring and control
- Client libraries for most popular programming languages

## How to configure RabbitMQ as an event destination

To configure RabbitMQ as a destination you must provide:

- A **Server URL** including the port number
- A **Username**
- A **Password**

**Exchange** is optional. If not provided, the default RabbitMQ exchange will be used.
