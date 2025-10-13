import type { Tenant } from '../../sdks/outpost-typescript/dist/commonjs/models/components/index';

export function createTenantId(): string {
  return `tenant_${Math.random().toString(36).substring(2, 15)}`;
}

export function createTenantData(overrides?: Partial<any>): any {
  return {
    id: createTenantId(),
    name: 'Test Tenant',
    ...overrides,
  };
}
