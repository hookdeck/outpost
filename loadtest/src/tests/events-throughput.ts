import http from "k6/http";
import { check } from "k6";
import { Counter, Rate } from "k6/metrics";
import redis from "k6/experimental/redis";
import { loadEventsConfig } from "../lib/config.ts";

const ENVIRONMENT = __ENV.ENVIRONMENT || "local";
const SCENARIO = __ENV.SCENARIO || "basic";

const config = await loadEventsConfig(
  `../../config/environments/${ENVIRONMENT}.json`,
  `../../config/scenarios/events-throughput/${SCENARIO}.json`
);

const API_KEY = __ENV.API_KEY;
if (!API_KEY) {
  throw new Error("API_KEY environment variable is required");
}

const TESTID = __ENV.TESTID;
if (!TESTID) {
  throw new Error("TESTID environment variable is required");
}

// Redis client for storing event IDs
// @ts-ignore - k6 Redis client types may not match connection string parameter
const redisClient = new redis.Client(config.env.redis);

// Custom metrics
const eventsPublished = new Counter("events_published");
const publishSuccessRate = new Rate("event_publish_success_rate");

// Default options that can be overridden by config
const defaultOptions = {
  thresholds: {
    http_req_duration: ["p(95)<1000"],
    http_req_failed: ["rate<0.01"],
    event_publish_success_rate: ["rate>=1.0"], // 100% of events must be published successfully
  },
  scenarios: {
    events: {
      executor: "constant-arrival-rate",
      rate: 100,
      timeUnit: "1s",
      duration: "30s",
      preAllocatedVUs: 10,
      maxVUs: 100,
    },
  },
  // HTTP configuration
  http: {
    // Disable connection reuse to avoid potential issues with keep-alive
    keepAlive: false,
    // Increase timeouts
    timeout: "30s",
    // Disable compression to reduce CPU usage
    compression: "none",
    // Disable redirects
    redirects: 0,
    // Disable cookies
    cookies: {
      enabled: false,
    },
  },
};

// Merge config options with defaults (config takes precedence)
export const options = {
  thresholds: {
    ...defaultOptions.thresholds,
    ...(config.scenario.options?.thresholds || {}),
  },
  scenarios: {
    events: {
      ...defaultOptions.scenarios.events,
      ...(config.scenario.options?.scenarios?.events || {}),
    },
  },
};

// Single tenant ID for all VUs
const tenantId = `test-tenant-${TESTID}`;

// Setup function that runs once at the beginning
export function setup() {
  const headers = {
    "Content-Type": "application/json",
    Authorization: `Bearer ${API_KEY}`,
  };

  // Create tenant
  const tenantResponse = http.put(
    `${config.env.api.baseUrl}/api/v1/${tenantId}`,
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
    `${config.env.api.baseUrl}/api/v1/${tenantId}/destinations`,
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

  // Clear any existing data for this test ID
  redisClient.del([`events:${TESTID}`]);
  redisClient.del([`events_sorted:${TESTID}`]);
  redisClient.del([`events_list:${TESTID}`]);
  redisClient.del([`events:${TESTID}:count`]);
  redisClient.set(`events:${TESTID}:count`, "0", 0);

  console.log(`🚀 Test setup complete for tenant: ${tenantId}`);
  console.log(`📊 Redis initialized at ${config.env.redis}`);

  return { tenantId };
}

// Main function executed by each VU
export default function (data: { tenantId: string }) {
  const headers = {
    "Content-Type": "application/json",
    Authorization: `Bearer ${API_KEY}`,
  };

  // Generate a unique event ID
  const eventId = `event-${TESTID}-${__VU}-${__ITER}`;

  // Record timestamp when event is sent
  const sentTimestamp = new Date().getTime();

  // Publish event with Shopify order payload
  const eventResponse = http.post(
    `${config.env.api.baseUrl}/api/v1/publish`,
    JSON.stringify({
      tenant_id: data.tenantId,
      topic: "user.created",
      eligible_for_retry: false,
      id: eventId,
      data: {
        iteration: __ITER,
        vu: __VU,
        timestamp: new Date().toISOString(),
        sent_at: sentTimestamp,
        order: {
          id: 820982911946154500,
        admin_graphql_api_id: "gid://shopify/Order/820982911946154508",
        app_id: null,
        browser_ip: null,
        buyer_accepts_marketing: true,
        cancel_reason: "customer",
        cancelled_at: "2021-12-31T19:00:00-05:00",
        cart_token: null,
        checkout_id: null,
        checkout_token: null,
        client_details: null,
        closed_at: null,
        confirmation_number: null,
        confirmed: false,
        contact_email: "jon@example.com",
        created_at: "2021-12-31T19:00:00-05:00",
        currency: "USD",
        current_subtotal_price: "398.00",
        current_subtotal_price_set: {
          shop_money: {
            amount: "398.00",
            currency_code: "USD"
          },
          presentment_money: {
            amount: "398.00",
            currency_code: "USD"
          }
        },
        current_total_additional_fees_set: null,
        current_total_discounts: "0.00",
        current_total_discounts_set: {
          shop_money: {
            amount: "0.00",
            currency_code: "USD"
          },
          presentment_money: {
            amount: "0.00",
            currency_code: "USD"
          }
        },
        current_total_duties_set: null,
        current_total_price: "398.00",
        current_total_price_set: {
          shop_money: {
            amount: "398.00",
            currency_code: "USD"
          },
          presentment_money: {
            amount: "398.00",
            currency_code: "USD"
          }
        },
        current_total_tax: "0.00",
        current_total_tax_set: {
          shop_money: {
            amount: "0.00",
            currency_code: "USD"
          },
          presentment_money: {
            amount: "0.00",
            currency_code: "USD"
          }
        },
        customer_locale: "en",
        device_id: null,
        discount_codes: [],
        email: "jon@example.com",
        estimated_taxes: false,
        financial_status: "voided",
        fulfillment_status: "pending",
        landing_site: null,
        landing_site_ref: null,
        location_id: null,
        merchant_of_record_app_id: null,
        name: "#9999",
        note: null,
        note_attributes: [],
        number: 234,
        order_number: 1234,
        order_status_url: "https://jsmith.myshopify.com/548380009/orders/123456abcd/authenticate?key=abcdefg",
        original_total_additional_fees_set: null,
        original_total_duties_set: null,
        payment_gateway_names: ["visa", "bogus"],
        phone: null,
        po_number: null,
        presentment_currency: "USD",
        processed_at: "2021-12-31T19:00:00-05:00",
        reference: null,
        referring_site: null,
        source_identifier: null,
        source_name: "web",
        source_url: null,
        subtotal_price: "388.00",
        subtotal_price_set: {
          shop_money: {
            amount: "388.00",
            currency_code: "USD"
          },
          presentment_money: {
            amount: "388.00",
            currency_code: "USD"
          }
        },
        tags: "tag1, tag2",
        tax_exempt: false,
        tax_lines: [],
        taxes_included: false,
        test: true,
        token: "123456abcd",
        total_discounts: "20.00",
        total_discounts_set: {
          shop_money: {
            amount: "20.00",
            currency_code: "USD"
          },
          presentment_money: {
            amount: "20.00",
            currency_code: "USD"
          }
        },
        total_line_items_price: "398.00",
        total_line_items_price_set: {
          shop_money: {
            amount: "398.00",
            currency_code: "USD"
          },
          presentment_money: {
            amount: "398.00",
            currency_code: "USD"
          }
        },
        total_outstanding: "398.00",
        total_price: "388.00",
        total_price_set: {
          shop_money: {
            amount: "388.00",
            currency_code: "USD"
          },
          presentment_money: {
            amount: "388.00",
            currency_code: "USD"
          }
        },
        total_shipping_price_set: {
          shop_money: {
            amount: "10.00",
            currency_code: "USD"
          },
          presentment_money: {
            amount: "10.00",
            currency_code: "USD"
          }
        },
        total_tax: "0.00",
        total_tax_set: {
          shop_money: {
            amount: "0.00",
            currency_code: "USD"
          },
          presentment_money: {
            amount: "0.00",
            currency_code: "USD"
          }
        },
        total_tip_received: "0.00",
        total_weight: 0,
        updated_at: "2021-12-31T19:00:00-05:00",
        user_id: null,
        billing_address: {
          first_name: "Steve",
          address1: "123 Shipping Street",
          phone: "555-555-SHIP",
          city: "Shippington",
          zip: "40003",
          province: "Kentucky",
          country: "United States",
          last_name: "Shipper",
          address2: null,
          company: "Shipping Company",
          latitude: null,
          longitude: null,
          name: "Steve Shipper",
          country_code: "US",
          province_code: "KY"
        },
        customer: {
          id: 115310627314723950,
          email: "john@example.com",
          created_at: null,
          updated_at: null,
          first_name: "John",
          last_name: "Smith",
          state: "disabled",
          note: null,
          verified_email: true,
          multipass_identifier: null,
          tax_exempt: false,
          phone: null,
          email_marketing_consent: {
            state: "not_subscribed",
            opt_in_level: null,
            consent_updated_at: null
          },
          sms_marketing_consent: null,
          tags: "",
          currency: "USD",
          tax_exemptions: [],
          admin_graphql_api_id: "gid://shopify/Customer/115310627314723954",
          default_address: {
            id: 715243470612851200,
            customer_id: 115310627314723950,
            first_name: null,
            last_name: null,
            company: null,
            address1: "123 Elm St.",
            address2: null,
            city: "Ottawa",
            province: "Ontario",
            country: "Canada",
            zip: "K2H7A8",
            phone: "123-123-1234",
            name: "",
            province_code: "ON",
            country_code: "CA",
            country_name: "Canada",
            default: true
          }
        },
        discount_applications: [],
        fulfillments: [],
        line_items: [
          {
            id: 866550311766439000,
            admin_graphql_api_id: "gid://shopify/LineItem/866550311766439020",
            attributed_staffs: [
              {
                id: "gid://shopify/StaffMember/902541635",
                quantity: 1
              }
            ],
            current_quantity: 1,
            fulfillable_quantity: 1,
            fulfillment_service: "manual",
            fulfillment_status: null,
            gift_card: false,
            grams: 567,
            name: "IPod Nano - 8GB",
            price: "199.00",
            price_set: {
              shop_money: {
                amount: "199.00",
                currency_code: "USD"
              },
              presentment_money: {
                amount: "199.00",
                currency_code: "USD"
              }
            },
            product_exists: true,
            product_id: 632910392,
            properties: [],
            quantity: 1,
            requires_shipping: true,
            sku: "IPOD2008PINK",
            taxable: true,
            title: "IPod Nano - 8GB",
            total_discount: "0.00",
            total_discount_set: {
              shop_money: {
                amount: "0.00",
                currency_code: "USD"
              },
              presentment_money: {
                amount: "0.00",
                currency_code: "USD"
              }
            },
            variant_id: 808950810,
            variant_inventory_management: "shopify",
            variant_title: null,
            vendor: null,
            tax_lines: [],
            duties: [],
            discount_allocations: []
          },
          {
            id: 141249953214522980,
            admin_graphql_api_id: "gid://shopify/LineItem/141249953214522974",
            attributed_staffs: [],
            current_quantity: 1,
            fulfillable_quantity: 1,
            fulfillment_service: "manual",
            fulfillment_status: null,
            gift_card: false,
            grams: 567,
            name: "IPod Nano - 8GB",
            price: "199.00",
            price_set: {
              shop_money: {
                amount: "199.00",
                currency_code: "USD"
              },
              presentment_money: {
                amount: "199.00",
                currency_code: "USD"
              }
            },
            product_exists: true,
            product_id: 632910392,
            properties: [],
            quantity: 1,
            requires_shipping: true,
            sku: "IPOD2008PINK",
            taxable: true,
            title: "IPod Nano - 8GB",
            total_discount: "0.00",
            total_discount_set: {
              shop_money: {
                amount: "0.00",
                currency_code: "USD"
              },
              presentment_money: {
                amount: "0.00",
                currency_code: "USD"
              }
            },
            variant_id: 808950810,
            variant_inventory_management: "shopify",
            variant_title: null,
            vendor: null,
            tax_lines: [],
            duties: [],
            discount_allocations: []
          }
        ],
        payment_terms: null,
        refunds: [],
        shipping_address: {
          first_name: "Steve",
          address1: "123 Shipping Street",
          phone: "555-555-SHIP",
          city: "Shippington",
          zip: "40003",
          province: "Kentucky",
          country: "United States",
          last_name: "Shipper",
          address2: null,
          company: "Shipping Company",
          latitude: null,
          longitude: null,
          name: "Steve Shipper",
          country_code: "US",
          province_code: "KY"
        },
        shipping_lines: [
          {
            id: 271878346596884000,
            carrier_identifier: null,
            code: null,
            discounted_price: "10.00",
            discounted_price_set: {
              shop_money: {
                amount: "10.00",
                currency_code: "USD"
              },
              presentment_money: {
                amount: "10.00",
                currency_code: "USD"
              }
            },
            is_removed: false,
            phone: null,
            price: "10.00",
            price_set: {
              shop_money: {
                amount: "10.00",
                currency_code: "USD"
              },
              presentment_money: {
                amount: "10.00",
                currency_code: "USD"
              }
            },
            requested_fulfillment_service_id: null,
            source: "shopify",
            title: "Generic Shipping",
            tax_lines: [],
            discount_allocations: []
          }
        ],
        },
      },
    }),
    { headers }
  );

  // Check if the event was published successfully (202 Accepted is the success status)
  const isSuccess = eventResponse.status === 202;

  // Record custom metrics
  publishSuccessRate.add(isSuccess);
  if (isSuccess) {
    eventsPublished.add(1);
  }

  check(eventResponse, {
    "event published": () => isSuccess,
  });

  if (!isSuccess) {
    console.error(
      `Failed to publish event: ${eventResponse.status} ${eventResponse.body}`
    );
    return;
  }

  // Store event ID in Redis
  if (redisClient) {
    // Add to Redis set (keep for backward compatibility)
    redisClient.sadd(`events:${TESTID}`, eventId);

    // Also add to a simple list that preserves insertion order
    // We're using lpush (push to head) so most recent events are at the front
    // @ts-ignore - Redis client types don't match correctly
    redisClient.lpush(`events_list:${TESTID}`, eventId);

    // Store the sent timestamp for latency calculation
    redisClient.set(
      `event:${TESTID}:${eventId}:sent_at`,
      sentTimestamp.toString(),
      0
    );

    // Increment event count
    redisClient.incr(`events:${TESTID}:count`);
  }
}

// Teardown function runs once at the end of the test
export function teardown(data: { tenantId: string }) {
  console.log(`📊 Test completed for tenant: ${data.tenantId}`);
  console.log(
    `📊 Events stored in Redis under keys: events:${TESTID} and events_list:${TESTID}`
  );
  console.log(
    `📊 To verify these events, run the events-verify test with TESTID=${TESTID}`
  );
}

// Each item is ~49 bytes:
// - id: "item_0" (~7 chars)
// - value: "value_0" (~8 chars)
// - timestamp: ISO string (~24 chars)
// - JSON structure (~10 chars)
// 125 items × 49 bytes = 6,125 bytes ≈ 6KB
function fillerPayload(count: number = 125) {
  return Array(count)
    .fill(null)
    .map((_, i) => ({
      id: `item_${i}`,
      value: `value_${i}`,
      timestamp: new Date().toISOString(),
    }));
}
