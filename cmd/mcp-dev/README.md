# mcp-dev

Experimental. A toolkit for developing and testing MCP Events (draft SEP-3415) servers, clients and webhook receivers against Outpost's `/mcp` routes. Interfaces can change.

## Try it

Needs only Docker. Stop any other Outpost dev stack first: `make up/mcp` binds host ports 3333 (API), 4500 (`serve`), 24222 and 28222 (NATS), 26379 (Redis), 28123 and 29000 (ClickHouse).

```sh
cp .env.dev .env                        # if you don't have them yet
cp .outpost.yaml.dev .outpost.yaml
make up/mcp
make mcp/dev ARGS=tour                  # the first run builds the image and compiles mcp-dev
```

`tour` runs in one terminal: it lists the events, subscribes with an argument, publishes a matching and a non-matching event (only one arrives, with a valid signature), refreshes, unsubscribes and publishes once more (nothing arrives). Each step prints the JSON-RPC call next to what the receiver got.

`make up/mcp` is `make up` plus:

- MCP Events on (`EXPERIMENTAL_MCP_ENABLED`), with five sample events from `cmd/mcp-dev/samples/events.json`
- http callbacks allowed to the stack's Docker network only, where the `mcp-dev` containers listen
- retries after 1, 3, 5 and 5 minutes
- a tenant `demo`

`make up` turns MCP Events off again.

`make mcp/dev ARGS=...` runs `mcp-dev` in a container on that network, with the repo mounted and a Go build cache. The container is named `mcp-dev-<subcommand>` (`mcp-dev-receive`, `mcp-dev-serve`, ...): Outpost and the other `mcp-dev` containers reach it by that name, and the default `--public-url`, `--server` and `--outpost-url` use it. A name can run once at a time. Paths in arguments (`--file`, `diff FILE`, `samples --out`) are relative to the repo root, which the container mounts.

## Drive it yourself

Three terminals, from the repo root:

```sh
# 1: a reference MCP Events server in front of Outpost (also at http://localhost:4500)
make mcp/dev ARGS=serve

# 2: a receiver; prints the callback URL and a secret
make mcp/dev ARGS=receive

# 3: act as a client, then publish
export MCP_TOKEN=demo
make mcp/dev ARGS="client discover"
make mcp/dev ARGS="client list"
make mcp/dev ARGS="client subscribe --event incident.created --arguments '{\"severity\":\"P1\"}' \
  --url http://mcp-dev-receive:4501/hook --secret <whsec_... secret printed in terminal 2>"
curl -s localhost:3333/api/v1/publish -H 'Authorization: Bearer apikey' -H 'Content-Type: application/json' \
  -d '{"tenant_id":"demo","topic":"incident.created","data":{"id":"inc_1","service":"checkout","severity":"P1"}}'
```

`client refresh` and `client unsubscribe` take the same flags. Terminal 2 prints the verification, then the event, each with its signature check. The verification appears only the first time a tenant subscribes to a callback URL within `EXPERIMENTAL_MCP_VERIFICATION_TTL_SECONDS` (24 h by default): Outpost caches the passed challenge in Redis, and the cache survives `make up`. To see it again, subscribe with another `MCP_TOKEN` or another `--url`. `receive --event-status 500` answers deliveries with an error to show retries; stopping it with Ctrl-C prints a summary and exits 1 if a delivery broke a rule.

`serve` treats a bearer token as the ID of the tenant it acts for, so `MCP_TOKEN=demo` subscribes in tenant `demo`. `--user NAME=TENANT[:TOPIC,...]` ties a token to a tenant instead and limits the `events/list` it sees to the topics.

`client` works against any MCP Events server: `--server` (`MCP_SERVER_URL`) takes its URL, `--token` (`MCP_TOKEN`) is sent as the bearer token, `--json` prints the raw response.

## check

```sh
make mcp/dev ARGS="check --publish-tenant demo"
```

Runs the webhook flow of the spec against a server (`--server`, default the `serve` address) and prints one line per rule: `PASS`, `FAIL`, `WARN` or `SKIP`, with the spec section behind each. Exit status 0 means no `FAIL`, 1 a `FAIL`, 2 a usage error. `--json` prints the report as JSON, so it can run in CI.

With `--publish-tenant` (publish mode) the check publishes through Outpost's `/publish`, so it also checks delivered data, retries, secret rotation, that unsubscribe stops delivery, argument filtering and isolation between two subscribers (two bearer tokens, by default the publish tenant and the same name followed by `-2`, each in its own tenant). Without it (wait mode) the check waits up to `--wait` (30s) for you to trigger the event upstream and skips those checks; this works against any server.

Publish mode assumes Outpost's argument semantics: each argument filters on the `data` field of the same name. The spec leaves argument meaning to the server, so those checks only apply to servers behind Outpost. The event, arguments and payloads come from the sample definitions; override them with `--event`, `--arguments`, `--data` and `--other-data`.

Outpost retries an MCP delivery 1 minute after the first attempt, so `delivery.retry` is skipped unless `--wait` is longer than that, for example `--wait 90s`.

A server that accepts the receiver's http callback (as `make up/mcp` does for its Docker network) gets a `WARN` on `subscribe.https-required`: the spec requires https.

`check` and `client` reach `serve` at `http://mcp-dev-serve:4500`, so start `serve` first.

`check --help` lists what isn't covered (poll and stream delivery, verification without a challenge, cursor replay, SSRF rules and more).

## Publish, diff and samples

```sh
make mcp/dev ARGS="publish --tenant demo --count 3 --sink"
```

publishes `--count` events per event name, with data built from each `payloadSchema`. Events come from the embedded sample, `--file` (an `events/list` result) or `--server`; `--event` limits the names, `--dry-run` prints the data. Outpost stores an event only when a destination matches it, so `--sink` creates a temporary webhook destination that accepts everything (`--sink-listen`, `--sink-url`) and deletes it at the end. Without a match `publish` warns.

```sh
make mcp/dev ARGS="diff exported.json --server http://mcp-dev-serve:4500/"
```

compares an exported `events/list` file with a server's list (or `diff FILE FILE2`), by event name. Exit status 0 means equal, 1 differences, printed with their path. `delivery` is ignored; `--ignore FIELD` replaces that list (repeat it, and include `delivery` to keep ignoring it).

`samples --out DIR` writes the sample `events/list` result (`events.json`); while `serve` runs it is also at `http://127.0.0.1:4500/samples/events.json`.

## Connecting to Outpost

`serve`, `check`, `tour` and `publish` reach Outpost with `--outpost-url` (`OUTPOST_URL`, default `http://localhost:3333/api/v1`), `--api-key` (`OUTPOST_API_KEY`, default `apikey`) and `--outpost-header 'Name: value'` (`OUTPOST_HEADERS`, repeatable; sent on every request, for a proxy in front of Outpost).

## Troubleshooting

- `container name ... is already in use`: another `make mcp/dev` with the same subcommand is running.
- `subscribe` fails with `CallbackEndpointError` (`connection_refused`, `timeout`): Outpost can't reach the receiver. Check that the stack is `make up/mcp` and that `--url` / `--public-url` use the `mcp-dev-<subcommand>` name of the receiving container.
- `challenge_failed`: the receiver's secret differs from the one in `subscribe`.
- `ResourceExhausted`: too many verifications for one tenant in a minute; wait, or use another `MCP_TOKEN`.

## Without Docker

`go run ./cmd/mcp-dev ARGS` runs the same commands on the host, with Go installed. The defaults then point at `localhost:3333` for Outpost and at `host.docker.internal` for the receiver, which suits an Outpost that runs in Docker with an allowlisted host address (`DESTINATIONS_PRIVATE_ADDRESS_ALLOWLIST`) instead of `make up/mcp`.
