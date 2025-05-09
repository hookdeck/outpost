import http from "k6/http";
import { check, sleep } from "k6";
import { Rate, Trend } from "k6/metrics";
import redis from "k6/experimental/redis";
import { loadEventsConfig } from "../lib/config.ts";

const ENVIRONMENT = __ENV.ENVIRONMENT || "local";
const SCENARIO = __ENV.SCENARIO || "basic";

// Only load config if we need the environment settings
const config = await loadEventsConfig(
  `../../config/environments/${ENVIRONMENT}.json`,
  `../../config/scenarios/events-verify/${SCENARIO}.json`
);

const API_KEY = __ENV.API_KEY;
if (!API_KEY) {
  throw new Error("API_KEY environment variable is required");
}

const TESTID = __ENV.TESTID;
if (!TESTID) {
  throw new Error("TESTID environment variable is required");
}

// Get max events to verify
const MAX_ITERATIONS = parseInt(__ENV.MAX_ITERATIONS || "1000", 10);

// Redis client for retrieving event IDs
// @ts-ignore - Redis client types don't match k6's implementation
const redisClient = new redis.Client(config.env.redis);

// The Redis set key containing all the events
const eventsSetKey = `events:${TESTID}`;

// Custom metrics
const verificationRate = new Rate("event_verification_rate");
const verificationTime = new Trend("event_verification_time");

// Latency metrics for different stages of event processing
const endToEndLatency = new Trend("end_to_end_event_latency"); // Publisher to Receiver (1-4)
const receiveLatency = new Trend("receive_latency"); // Publisher to Outpost receipt (1-2)
const internalLatency = new Trend("internal_outpost_event_latency"); // Outpost receipt to delivery start (2-3)

// Default options for the test - these are our actual settings
export const options = {
  thresholds: {
    checks: ["rate>=1.0"], // 100% of checks must pass
    event_verification_rate: ["rate>=1.0"], // 100% of events must be verified
    end_to_end_event_latency: ["p(95)<1000"], // 95% of events should be processed in under 1 second
    receive_latency: ["p(95)<500"], // 95% of events should be received by Outpost within 500ms
    internal_outpost_event_latency: ["p(95)<500"], // 95% of events should be processed internally within 500ms
  },
  scenarios: {
    verify: {
      executor: "shared-iterations",
      iterations: MAX_ITERATIONS,
      vus: 10,
      maxDuration: "5m",
    },
  },
};

// Simple verification function - each VU pops one event from the set and verifies it
export default async function () {
  try {
    // Instead of atomically popping a random event, get the latest events first
    // Try to get an event from the ordered list (latest events are at the front)
    const eventId = await redisClient.lpop(`events_list:${TESTID}`);

    // If no event was found in the list, try the regular set as fallback
    let finalEventId = eventId;
    if (!finalEventId) {
      finalEventId = await redisClient.spop(eventsSetKey);
    }

    // If no event ID was found, we're done
    if (!finalEventId) {
      return;
    }

    const verificationUrl = `${config.env.mockWebhook.url}/events/${finalEventId}`;

    // Get event details from Outpost API to get timestamp when event was received by Outpost
    const outpostEventUrl = `${config.env.api.baseUrl}/api/v1/test-tenant-${TESTID}/events/${finalEventId}`;

    // Parse verification poll timeout to seconds
    const timeoutStr = config.env.mockWebhook.verificationPollTimeout;
    let verificationTimeoutSec = 5; // Default 5 seconds

    if (timeoutStr.endsWith("s")) {
      verificationTimeoutSec = parseInt(timeoutStr.slice(0, -1), 10);
    } else if (timeoutStr.endsWith("ms")) {
      verificationTimeoutSec = parseInt(timeoutStr.slice(0, -2), 10) / 1000;
    }

    // Poll until timeout
    let verified = false;
    let receivedTimestamp = null;
    let deliveryStartTimestamp = null;
    let successfulResponse = null; // Capture the successful verification response
    const pollInterval = 1; // 1 second between checks
    const startTime = new Date().getTime();

    for (
      let elapsed = 0;
      elapsed < verificationTimeoutSec;
      elapsed += pollInterval
    ) {
      const verificationResponse = http.get(verificationUrl);

      if (verificationResponse.status === 200) {
        verified = true;
        successfulResponse = verificationResponse; // Store the successful response
        // Get received timestamp from response
        try {
          const responseBody = verificationResponse.body;
          if (responseBody) {
            const responseData = JSON.parse(responseBody.toString());
            // Extract received_at timestamp if available
            if (responseData && responseData.received_at) {
              // Handle ISO timestamp format (e.g. "2025-04-28T13:35:17.115820003Z")
              try {
                const receivedDate = new Date(responseData.received_at);
                // Convert to milliseconds timestamp for comparison
                if (!isNaN(receivedDate.getTime())) {
                  receivedTimestamp = receivedDate.getTime();
                } else {
                  console.error(
                    `Invalid received_at timestamp format: ${responseData.received_at}`
                  );
                }
              } catch (parseError) {
                console.error(
                  `Failed to parse received_at timestamp: ${parseError}`
                );
              }
            }

            // Extract delivery start timestamp from headers (X-Acme-Timestamp or X-Outpost-Timestamp)
            if (responseData && responseData.headers) {
              const timestampHeader =
                responseData.headers["X-Acme-Timestamp"] ||
                responseData.headers["X-Outpost-Timestamp"];
              if (timestampHeader) {
                const timestamp = parseInt(timestampHeader, 10);
                if (!isNaN(timestamp)) {
                  // Convert seconds to milliseconds if needed
                  deliveryStartTimestamp =
                    timestamp.toString().length === 10
                      ? timestamp * 1000
                      : timestamp;
                }
              }
            }
          }
        } catch (e) {
          console.error(`Failed to parse verification response: ${e}`);
        }
        // Calculate and record verification time
        const verificationDuration = new Date().getTime() - startTime;
        verificationTime.add(verificationDuration);
        break;
      }

      if (elapsed + pollInterval < verificationTimeoutSec) {
        sleep(pollInterval);
      }
    }

    // Calculate event latencies if verification was successful
    if (verified) {
      try {
        // Try to get sent timestamp from Redis first
        // @ts-ignore: Redis types don't handle null returns properly
        const sentTimestampStr = await redisClient.get(
          `event:${TESTID}:${finalEventId}:sent_at`
        );

        // Parse sent timestamp if available from Redis
        let sentTimestamp = null;
        if (sentTimestampStr !== null) {
          const parsedTimestamp = parseInt(sentTimestampStr.toString(), 10);
          if (!isNaN(parsedTimestamp) && parsedTimestamp > 0) {
            // Keep millisecond precision
            sentTimestamp = parsedTimestamp;
          }
        }

        // If no sent timestamp from Redis, try to get it from the response payload
        if (!sentTimestamp && successfulResponse && successfulResponse.body) {
          try {
            const responseBody = successfulResponse.body.toString();
            const responseData = JSON.parse(responseBody);

            // Check for sent_at in the payload
            if (
              responseData &&
              responseData.payload &&
              responseData.payload.sent_at
            ) {
              const payloadSentAt = Number(responseData.payload.sent_at);
              if (!isNaN(payloadSentAt) && payloadSentAt > 0) {
                // Keep millisecond precision
                sentTimestamp = payloadSentAt;
                console.error(
                  `Using sent_at from payload for event ${finalEventId}`
                );
              }
            }
          } catch (parseError) {
            console.error(
              `Failed to extract sent_at from payload: ${parseError}`
            );
          }
        }

        // Fetch the event details from Outpost API to get the timestamp when event was received
        let outpostReceivedTimestamp = null;
        try {
          const outpostEventResponse = http.get(outpostEventUrl, {
            headers: {
              Authorization: `Bearer ${API_KEY}`,
            },
          });
          if (
            outpostEventResponse.status === 200 &&
            outpostEventResponse.body
          ) {
            const outpostEventData = JSON.parse(
              outpostEventResponse.body.toString()
            );
            if (outpostEventData && outpostEventData.time) {
              const outpostTimeDate = new Date(outpostEventData.time);
              if (!isNaN(outpostTimeDate.getTime())) {
                // Keep millisecond precision
                outpostReceivedTimestamp = outpostTimeDate.getTime();
              }
            }
          }
        } catch (outpostErr) {
          console.error(`Failed to fetch Outpost event details: ${outpostErr}`);
        }

        // Keep receivedTimestamp in milliseconds
        let receivedTimestampMs = receivedTimestamp;

        // Parse delivery start timestamp
        let deliveryStartTimestampMs = null;
        if (deliveryStartTimestamp) {
          // If timestamp has 10 digits, it's in seconds - convert to milliseconds
          if (deliveryStartTimestamp.toString().length === 10) {
            deliveryStartTimestampMs = deliveryStartTimestamp * 1000;
          } else {
            // Otherwise, assume it's already in milliseconds
            deliveryStartTimestampMs = deliveryStartTimestamp;
          }
        }

        // Calculate and record end-to-end latency (1-4) in milliseconds
        if (sentTimestamp && receivedTimestampMs) {
          const e2eLatency = receivedTimestampMs - sentTimestamp;
          if (!isNaN(e2eLatency) && e2eLatency >= 0) {
            endToEndLatency.add(e2eLatency);
          } else {
            console.error(
              `Invalid end-to-end latency calculation for event ${finalEventId}: ${e2eLatency}`
            );
          }
        } else {
          if (!sentTimestamp) {
            console.error(`Missing sent timestamp for event ${finalEventId}`);
          }
          if (!receivedTimestampMs) {
            console.error(
              `Missing received timestamp for event ${finalEventId}`
            );
          }
        }

        // Calculate and record receive latency (1-2) in milliseconds
        if (sentTimestamp && outpostReceivedTimestamp) {
          const recLatency = outpostReceivedTimestamp - sentTimestamp;
          if (!isNaN(recLatency) && recLatency >= 0) {
            receiveLatency.add(recLatency);
          } else {
            console.error(
              `Invalid receive latency calculation for event ${finalEventId}: ${recLatency}`
            );
          }
        }

        // Calculate and record internal outpost latency (2-3) in milliseconds
        if (outpostReceivedTimestamp && deliveryStartTimestampMs) {
          const intLatency =
            deliveryStartTimestampMs - outpostReceivedTimestamp;
          if (!isNaN(intLatency)) {
            // If negative, treat as 0 latency (clock synchronization issue)
            const adjustedLatency = Math.max(0, intLatency);
            internalLatency.add(adjustedLatency);
          } else {
            console.error(
              `Invalid internal latency calculation for event ${finalEventId}: ${intLatency}`
            );
          }
        }

        // After all latency calculations, log a summary for this event
        if (sentTimestamp) {
          // Calculate the expected sum to verify our metrics add up
          const receiveLatencyVal =
            outpostReceivedTimestamp && sentTimestamp
              ? outpostReceivedTimestamp - sentTimestamp
              : 0;
          const internalLatencyVal =
            deliveryStartTimestampMs && outpostReceivedTimestamp
              ? Math.max(0, deliveryStartTimestampMs - outpostReceivedTimestamp)
              : 0;
          const deliveryLatencyVal =
            receivedTimestampMs && deliveryStartTimestampMs
              ? receivedTimestampMs - deliveryStartTimestampMs
              : 0;

          const totalCalculatedLatency =
            receiveLatencyVal + internalLatencyVal + deliveryLatencyVal;

          // console.log(`Event ${finalEventId} latency summary (milliseconds):
          //   Sent: ${
          //     sentTimestamp ? new Date(sentTimestamp).toISOString() : "missing"
          //   }
          //   Received by Outpost: ${
          //     outpostReceivedTimestamp
          //       ? new Date(outpostReceivedTimestamp).toISOString()
          //       : "missing"
          //   }
          //   Delivery started: ${
          //     deliveryStartTimestampMs
          //       ? new Date(deliveryStartTimestampMs).toISOString()
          //       : "missing"
          //   }
          //   Received by endpoint: ${
          //     receivedTimestampMs
          //       ? new Date(receivedTimestampMs).toISOString()
          //       : "missing"
          //   }
          //   End-to-end: ${
          //     receivedTimestampMs && sentTimestamp
          //       ? receivedTimestampMs - sentTimestamp
          //       : "N/A"
          //   }
          //   Receive: ${
          //     outpostReceivedTimestamp && sentTimestamp
          //       ? outpostReceivedTimestamp - sentTimestamp
          //       : "N/A"
          //   }
          //   Internal: ${
          //     deliveryStartTimestampMs && outpostReceivedTimestamp
          //       ? Math.max(
          //           0,
          //           deliveryStartTimestampMs - outpostReceivedTimestamp
          //         )
          //       : "N/A"
          //   }
          //   Delivery: ${
          //     receivedTimestampMs && deliveryStartTimestampMs
          //       ? receivedTimestampMs - deliveryStartTimestampMs
          //       : "N/A"
          //   }
          //   Total calculated: ${totalCalculatedLatency}
          //   Diff from E2E: ${
          //     receivedTimestampMs && sentTimestamp
          //       ? receivedTimestampMs - sentTimestamp - totalCalculatedLatency
          //       : "N/A"
          //   }`);
        }
      } catch (latencyErr) {
        console.error(`Failed to calculate latency: ${latencyErr}`);
      }

      // Clean up Redis key for sent timestamp
      redisClient.del([`event:${TESTID}:${finalEventId}:sent_at`]);
    }

    // Add to verification rate metric (true if verified, false if not)
    verificationRate.add(verified);

    // Record verification result
    check(null, {
      "event verified": () => verified,
    });
  } catch (err) {
    console.error(`Error during verification: ${err}`);
    // Throw to fail the test and prevent further iterations
    throw new Error(`Verification failed: ${err}`);
  }
}
