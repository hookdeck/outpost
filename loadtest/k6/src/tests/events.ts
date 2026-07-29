import http from "k6/http";
import { check } from "k6";
import { sleep } from "k6";
import { loadEventsConfig } from "../lib/config.ts";

const ENVIRONMENT = __ENV.ENVIRONMENT || "local";
const SCENARIO = __ENV.SCENARIO || "basic";

const config = await loadEventsConfig(
  `../../config/environments/${ENVIRONMENT}.json`,
  `../../config/scenarios/events/${SCENARIO}.json`
);

const API_KEY = __ENV.API_KEY;
if (!API_KEY) {
  throw new Error("API_KEY environment variable is required");
}

const TESTID = __ENV.TESTID;
if (!TESTID) {
  throw new Error("TESTID environment variable is required");
}

// Default options that can be overridden by config
const defaultOptions = {
  thresholds: {
    http_req_duration: ["p(95)<1000"],
    http_req_failed: ["rate<0.01"],
  },
  scenarios: {
    events: {
      executor: "shared-iterations",
      iterations: 1,
      vus: 1,
      maxDuration: "30s",
    },
  },
};

// Merge config options with defaults (config takes precedence)
export const options = {
  ...defaultOptions,
  ...config.scenario.options,
};

export default function (): void {
  const tenantId = `test-tenant-${TESTID}-${__VU}`;
  const headers = {
    "Content-Type": "application/json",
    Authorization: `Bearer ${API_KEY}`,
  };

  // Create tenant and destination only on first iteration
  if (__ITER === 0) {
    // Create tenant
    const tenantResponse = http.put(
      `${config.env.api.baseUrl}/api/v1/tenants/${tenantId}`,
      JSON.stringify({}),
      { headers }
    );

    check(tenantResponse, {
      "tenant created": (r) => r.status === 201,
    });

    if (tenantResponse.status !== 201) {
      throw new Error(
        `Unexpected tenant creation status: ${tenantResponse.status}. Response: ${tenantResponse.body}`
      );
    }

    // Create destination
    const destinationResponse = http.post(
      `${config.env.api.baseUrl}/api/v1/tenants/${tenantId}/destinations`,
      JSON.stringify({
        type: "webhook",
        topics: ["user.created"],
        config: {
          url: `${config.env.mockWebhook.destinationUrl}/webhook`,
        },
      }),
      { headers }
    );

    check(destinationResponse, {
      "destination created": (r) => r.status === 201,
    });

    if (destinationResponse.status !== 201) {
      throw new Error(
        `Failed to create destination: ${destinationResponse.status} ${destinationResponse.body}`
      );
    }

    if (!destinationResponse.body) {
      throw new Error("Failed to create destination: no response body");
    }
  }

  // Generate a unique event ID for verification
  const eventId = `event-${TESTID}-${__VU}-${__ITER}`;

  // Publish event (every iteration)
  const eventResponse = http.post(
    `${config.env.api.baseUrl}/api/v1/publish`,
    JSON.stringify({
      tenant_id: tenantId,
      topic: "user.created",
      eligible_for_retry: false,
      id: eventId,
      data: {
        iteration: __ITER,
        tenant_id: tenantId,
      },
    }),
    { headers }
  );

  check(eventResponse, {
    "event published": (r) => r.status === 202,
  });

  if (eventResponse.status !== 202) {
    throw new Error(
      `Failed to publish event: ${eventResponse.status} ${eventResponse.body}`
    );
  }

  // Verify event delivery to webhook using polling strategy
  const verificationUrl = `${config.env.mockWebhook.url}/events/${eventId}`;

  // Parse verification poll timeout to seconds
  const timeoutStr = config.env.mockWebhook.verificationPollTimeout;
  let verificationTimeoutSec = 5; // Default 5 seconds

  if (timeoutStr.endsWith("s")) {
    verificationTimeoutSec = parseInt(timeoutStr.slice(0, -1), 10);
  } else if (timeoutStr.endsWith("ms")) {
    verificationTimeoutSec = parseInt(timeoutStr.slice(0, -2), 10) / 1000;
  }

  // Poll every second up to the verification timeout
  let verified = false;
  const pollInterval = 1; // 1 second between checks

  for (
    let elapsed = 0;
    elapsed < verificationTimeoutSec;
    elapsed += pollInterval
  ) {
    const verificationResponse = http.get(verificationUrl);

    if (verificationResponse.status === 200) {
      verified = true;
      console.log(`✓ Event ${eventId} verified after ${elapsed} seconds`);
      break;
    }

    if (elapsed + pollInterval < verificationTimeoutSec) {
      console.log(
        `Event not yet delivered, polling again in ${pollInterval}s... (${elapsed}/${verificationTimeoutSec}s elapsed)`
      );
      sleep(pollInterval);
    }
  }

  // Record verification result in k6 metrics
  check(null, {
    "event delivered to webhook": () => verified,
  });

  if (!verified) {
    console.log(
      `⨯ Event ${eventId} not delivered after ${verificationTimeoutSec} seconds of polling`
    );
    throw new Error(
      `Event verification failed: Event ${eventId} was not delivered to webhook within ${verificationTimeoutSec} seconds`
    );
  }
}
