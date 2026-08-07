import http from "k6/http";
import { check } from "k6";
import { loadHealthConfig } from "../lib/config.ts";

const config = await loadHealthConfig(
  "../../config/environments/local.json",
  "../../config/scenarios/management/health.json"
);

export const options = {
  thresholds: config.scenario.thresholds,
  scenarios: {
    health: {
      executor: "constant-arrival-rate",
      rate: config.scenario.baseRPS,
      timeUnit: "1s",
      duration: config.scenario.duration,
      preAllocatedVUs: 1,
      maxVUs: 1,
    },
  },
};

export default function (): void {
  const response = http.get(`${config.env.api.baseUrl}/api/v1/healthz`);

  check(response, {
    "status is 200": (r) => r.status === 200,
    "response time < 1000ms": (r) => r.timings.duration < 1000,
  });
}
