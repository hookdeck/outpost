# mcp-dev

Experimental. A toolkit for developing and testing MCP Events (draft SEP-3415) servers, clients and webhook receivers against Outpost's `/mcp` routes. Interfaces can change.

## Try it

```sh
cp .env.dev .env        # if you don't have a .env yet
make up/mcp
make mcp/dev ARGS=tour
```

`tour` runs in one terminal: it lists the events, subscribes with an argument, publishes a matching and a non-matching event (only one arrives, with a valid signature), refreshes, unsubscribes and publishes once more (nothing arrives). Each step prints the JSON-RPC call next to what the receiver got.

`make up/mcp` is `make up` plus:

- MCP Events on (`EXPERIMENTAL_MCP_ENABLED`), with five sample events from `cmd/mcp-dev/samples/events.json`
- http callbacks allowed to `host.docker.internal` only, where `mcp-dev` listens
- retries after 1, 2 and 5 seconds
- a tenant `demo`

`make up` turns MCP Events off again.

## Drive it yourself

Three terminals, from the repo root:

```sh
# 1: a reference MCP Events server in front of Outpost
make mcp/dev ARGS=serve

# 2: a receiver; prints the callback URL and a secret
make mcp/dev ARGS=receive

# 3: act as a client, then publish
export MCP_PRINCIPAL=alice
make mcp/dev ARGS="client discover"
make mcp/dev ARGS="client list"
make mcp/dev ARGS="client subscribe --event incident.created --arguments '{\"severity\":\"P1\"}' \
  --url http://host.docker.internal:4501/hook --secret <secret from terminal 2>"
curl -s localhost:3333/api/v1/publish -H 'Authorization: Bearer apikey' -H 'Content-Type: application/json' \
  -d '{"tenant_id":"demo","topic":"incident.created","data":{"id":"inc_1","service":"checkout","severity":"P1"}}'
```

`client refresh` and `client unsubscribe` take the same flags. Terminal 2 prints the verification, then the event, each with its signature check. `receive --event-status 500` answers deliveries with an error to show retries; stopping it with Ctrl-C prints a summary and exits 1 if a delivery broke a rule.

Without `--principal`, any bearer token acts for the tenant given to `serve --tenant` (default `demo`). `--principal NAME=TENANT[:TOPIC,...]` limits a token to a tenant and topics.

`client` works against any MCP Events server: `--server` (`MCP_SERVER_URL`) takes its URL, `--principal` (`MCP_PRINCIPAL`) is sent as the bearer token, `--json` prints the raw response.

## check

```sh
make mcp/dev ARGS="check --publish-tenant demo"
```

Runs the webhook flow of the spec against a server (`--server`, default the `serve` address) and prints one line per rule: `PASS`, `FAIL`, `WARN` or `SKIP`, with the spec section behind each. Exit status 0 means no `FAIL`, 1 a `FAIL`, 2 a usage error. `--json` prints the report as JSON, so it can run in CI.

With `--publish-tenant` (publish mode) the check publishes through Outpost's `/publish`, so it also checks delivered data, retries, secret rotation, that unsubscribe stops delivery, argument filtering and isolation between two principals. Without it (wait mode) the check waits up to `--wait` (30s) for you to trigger the event upstream and skips those checks; this works against any server.

Publish mode assumes Outpost's argument semantics: each argument filters on the `data` field of the same name. The spec leaves argument meaning to the server, so those checks only apply to servers behind Outpost. The event, arguments and payloads come from the sample definitions; override them with `--event`, `--arguments`, `--data` and `--other-data`.

A server that accepts the receiver's http callback (as `make up/mcp` does for the host) gets a `WARN` on `subscribe.https-required`: the spec requires https.

`check --help` lists what isn't covered (poll and stream delivery, verification without a challenge, cursor replay, SSRF rules and more).

## Publish, diff and samples

```sh
make mcp/dev ARGS="publish --tenant demo --count 3 --sink"
```

publishes `--count` events per event name, with data built from each `payloadSchema`. Events come from the embedded sample, `--file` (an `events/list` result) or `--server`; `--event` limits the names, `--dry-run` prints the data. Outpost stores an event only when a destination matches it, so `--sink` creates a temporary webhook destination that accepts everything (`--sink-listen`, `--sink-url`) and deletes it at the end. Without a match `publish` warns.

```sh
make mcp/dev ARGS="diff exported.json --server http://127.0.0.1:4500/"
```

compares an exported `events/list` file with a server's list (or `diff FILE FILE2`), by event name. Exit status 0 means equal, 1 differences, printed with their path. `delivery` is ignored; `--ignore FIELD` replaces that list (repeat it, and include `delivery` to keep ignoring it).

`samples --out DIR` writes the sample `events/list` result (`events.json`); while `serve` runs it is also at `http://127.0.0.1:4500/samples/events.json`.

## Connecting to Outpost

`serve`, `check`, `tour` and `publish` reach Outpost with `--outpost-url` (`OUTPOST_URL`, default `http://localhost:3333/api/v1`), `--api-key` (`OUTPOST_API_KEY`, default `apikey`) and `--outpost-header 'Name: value'` (`OUTPOST_HEADERS`, repeatable; sent on every request, for a proxy in front of Outpost).

## Troubleshooting

- `can't listen on :4501`: something holds the port, often `mcp-dev receive`.
- `subscribe` fails with `CallbackEndpointError` (`connection_refused`, `timeout`): Outpost can't reach the receiver. Check that the stack is `make up/mcp` and that a host firewall isn't blocking Docker to the host on port 4501.
- `challenge_failed`: the receiver's secret differs from the one in `subscribe`.
- `ResourceExhausted`: too many verifications for one principal in a minute; use another `--principal`.
