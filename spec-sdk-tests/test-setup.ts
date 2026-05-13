/**
 * Mocha root hook plugin: optional delay between tests to avoid API rate limits (e.g. 429).
 * Set TEST_DELAY_MS in .env (e.g. 50) to add that many milliseconds before each test.
 * When unset or 0, no delay is applied.
 */
const delayMs = Math.max(0, Number(process.env.TEST_DELAY_MS) || 0);

export const mochaHooks = {
  beforeEach:
    delayMs > 0
      ? [
          async function (this: Mocha.Context) {
            await new Promise((r) => setTimeout(r, delayMs));
          },
        ]
      : [],
};
