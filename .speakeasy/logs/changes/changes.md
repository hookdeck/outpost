## Typescript SDK Changes:
* `outpost.destinations.update()`: `request.body` **Changed** (Breaking ⚠️)
    - `union(DestinationUpdateAWSKinesis)` **Removed** (Breaking ⚠️)
    - `union(DestinationUpdateAWSS3)` **Removed** (Breaking ⚠️)
    - `union(DestinationUpdateAWSSQS)` **Removed** (Breaking ⚠️)
    - `union(DestinationUpdateAzureServiceBus)` **Removed** (Breaking ⚠️)
    - `union(DestinationUpdateGCPPubSub)` **Removed** (Breaking ⚠️)
    - `union(DestinationUpdateHookdeck)` **Removed** (Breaking ⚠️)
    - `union(DestinationUpdateKafka)` **Removed** (Breaking ⚠️)
    - `union(DestinationUpdateRabbitMQ)` **Removed** (Breaking ⚠️)
    - `union(DestinationUpdateWebhook)` **Removed** (Breaking ⚠️)
    - `union(aws_kinesis)` **Added**
    - `union(aws_s3)` **Added**
    - `union(aws_sqs)` **Added**
    - `union(azure_servicebus)` **Added**
    - `union(gcp_pubsub)` **Added**
    - `union(hookdeck)` **Added**
    - `union(kafka)` **Added**
    - `union(rabbitmq)` **Added**
    - `union(webhook)` **Added**
