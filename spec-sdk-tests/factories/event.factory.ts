import type { PublishRequest } from '../../sdks/outpost-typescript/dist/commonjs/models/components/index';

export function createEventPayload(overrides?: Partial<PublishRequest>): PublishRequest {
  return {
    topic: 'user.created',
    data: {
      id: 'user_123',
      name: 'Test User',
      email: 'test@example.com',
    },
    ...overrides,
  };
}
