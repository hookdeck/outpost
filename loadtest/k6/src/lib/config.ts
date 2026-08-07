import { open } from "k6/experimental/fs";
import { Options } from "k6/options";

interface ApiConfig {
  baseUrl: string;
  timeout: string;
}

interface MockWebhookConfig {
  url: string;
  destinationUrl: string;
  verificationPollTimeout: string;
}

interface EnvironmentConfig {
  name: string;
  api: ApiConfig;
  mockWebhook: MockWebhookConfig;
  redis: string;
}

interface Thresholds {
  http_req_duration: string[];
  http_req_failed: string[];
}

// Health test config
interface HealthScenarioConfig {
  name: string;
  pattern: string;
  baseRPS: number;
  duration: string;
  thresholds: Thresholds;
}

interface HealthConfig {
  env: EnvironmentConfig;
  scenario: HealthScenarioConfig;
}

// Events test config
interface ExecutorOptions {
  iterations?: number;
  vus?: number;
  maxDuration?: string;
  rate?: number;
  timeUnit?: string;
  duration?: string;
  preAllocatedVUs?: number;
  maxVUs?: number;
}

interface EventsScenarioConfig {
  options?: Options;
}

interface EventsConfig {
  env: EnvironmentConfig;
  scenario: EventsScenarioConfig;
}

async function loadJsonFile(path: string): Promise<any> {
  const file = await open(path);
  const buffer = new Uint8Array(1024);
  const bytesRead = await file.read(buffer);

  return JSON.parse(
    String.fromCharCode.apply(null, Array.from(buffer.slice(0, bytesRead)))
  );
}

export async function loadHealthConfig(
  envPath: string,
  scenarioPath: string
): Promise<HealthConfig> {
  const env = await loadJsonFile(envPath);
  const scenario = await loadJsonFile(scenarioPath);

  return { env, scenario };
}

export async function loadEventsConfig(
  envPath: string,
  scenarioPath: string
): Promise<EventsConfig> {
  const env = await loadJsonFile(envPath);
  const scenario = await loadJsonFile(scenarioPath);

  return { env, scenario };
}
