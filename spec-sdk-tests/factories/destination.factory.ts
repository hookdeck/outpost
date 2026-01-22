import type {
  DestinationCreateWebhook,
  DestinationCreateAWSSQS,
  DestinationCreateRabbitMQ,
  DestinationCreateHookdeck,
  DestinationCreateAWSKinesis,
  DestinationCreateAzureServiceBus,
  DestinationCreateAwss3,
  DestinationCreateGCPPubSub,
  DestinationCreateCloudflareQueues,
} from '../../sdks/outpost-typescript/dist/commonjs/models/components/index';

export function createWebhookDestination(
  overrides?: Partial<DestinationCreateWebhook>
): DestinationCreateWebhook {
  return {
    type: 'webhook',
    topics: ['*'],
    config: {
      url: 'https://example.com/webhook',
    },
    ...overrides,
  };
}

export function createAwsSqsDestination(
  overrides?: Partial<DestinationCreateAWSSQS>
): DestinationCreateAWSSQS {
  return {
    type: 'aws_sqs',
    topics: ['*'],
    config: {
      queueUrl: 'https://sqs.us-east-1.amazonaws.com/123456789012/my-queue',
    },
    credentials: {
      key: 'AKIAIOSFODNN7EXAMPLE',
      secret: 'wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY',
    },
    ...overrides,
  };
}

export function createRabbitMqDestination(
  overrides?: Partial<DestinationCreateRabbitMQ>
): DestinationCreateRabbitMQ {
  return {
    type: 'rabbitmq',
    topics: ['*'],
    config: {
      serverUrl: 'host.com:5672',
      exchange: 'my-exchange',
    },
    credentials: {
      username: 'user',
      password: 'pass',
    },
    ...overrides,
  };
}

export function createHookdeckDestination(
  overrides?: Partial<DestinationCreateHookdeck>
): DestinationCreateHookdeck {
  // Create a valid Hookdeck token format: base64 encoded "source_id:signing_key"
  // This will pass ParseHookdeckToken but fail VerifyHookdeckToken (expected for tests)
  const validToken = Buffer.from('src_test123:test_signing_key').toString('base64');

  return {
    type: 'hookdeck',
    topics: ['*'],
    credentials: {
      token: validToken,
    },
    ...overrides,
  };
}

export function createAwsKinesisDestination(
  overrides?: Partial<DestinationCreateAWSKinesis>
): DestinationCreateAWSKinesis {
  return {
    type: 'aws_kinesis',
    topics: ['*'],
    config: {
      streamName: 'my-stream',
      region: 'us-east-1',
    },
    credentials: {
      key: 'AKIAIOSFODNN7EXAMPLE',
      secret: 'wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY',
    },
    ...overrides,
  };
}

export function createAzureServiceBusDestination(
  overrides?: Partial<DestinationCreateAzureServiceBus>
): DestinationCreateAzureServiceBus {
  return {
    type: 'azure_servicebus',
    topics: ['*'],
    config: {
      name: 'my-queue',
    },
    credentials: {
      connectionString:
        'Endpoint=sb://namespace.servicebus.windows.net/;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=key',
    },
    ...overrides,
  };
}

export function createAwsS3Destination(
  overrides?: Partial<DestinationCreateAwss3>
): DestinationCreateAwss3 {
  return {
    type: 'aws_s3',
    topics: ['*'],
    config: {
      bucket: 'my-bucket',
      region: 'us-east-1',
    },
    credentials: {
      key: 'AKIAIOSFODNN7EXAMPLE',
      secret: 'wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY',
    },
    ...overrides,
  };
}

export function createGcpPubSubDestination(
  overrides?: Partial<DestinationCreateGCPPubSub>
): DestinationCreateGCPPubSub {
  return {
    type: 'gcp_pubsub',
    topics: ['*'],
    config: {
      projectId: 'my-project',
      topic: 'my-topic',
    },
    credentials: {
      serviceAccountJson: '{"type":"service_account","project_id":"my-project"}',
    },
    ...overrides,
  };
}

export function createCloudflareQueuesDestination(
  overrides?: Partial<DestinationCreateCloudflareQueues>
): DestinationCreateCloudflareQueues {
  return {
    type: 'cloudflare_queues',
    topics: ['*'],
    config: {
      accountId: 'abc123def456',
      queueId: 'my-queue-id',
    },
    credentials: {
      apiToken: 'cf-api-token-example',
    },
    ...overrides,
  };
}
