# Adding a provider

A provider connects mqcheck to one broker. It knows the broker (its admin API and documented semantics) and how an operator configures Outpost for it. It knows nothing about how the consumer receives, limits or prefetches messages: that's what mqcheck measures. Cases, runner, worker and report don't change when a provider is added.

## Checklist

1. **One file**, `internal/mqcheck/providers/<name>.go`, that calls `provider.Register` from `init()`. `awssqs.go` is the reference; `gcppubsub.go` shows an emulator/real switch.
2. **Settings**: environment variables the provider reads (`MQCHECK_<BROKER>_*`), each with a default that points at the local broker and a description. Real-broker access is configuration only: no account, project or region in code. Mark secrets `Secret: true`.
3. **Provider methods** (see `internal/mqcheck/provider/provider.go`):
   - `Provision` / `Teardown`: queue + dead-letter queue named from `QueueSpec.Name`, with the spec's visibility timeout and max attempts. Use Outpost's own `mqinfra` when it supports the broker, so test queues look like the ones Outpost creates.
   - `Sweep`: list resources and delete those for which `provider.ShouldSweep(name, prefix, olderThan)` is true (it checks the prefix and reads the creation time from the name). `prefix` arrives bare, without a trailing `-`. Skip anything you can't judge.
   - `WorkerEnv`: the Outpost environment variables that point the delivery queue at the target, as an operator would set them.
   - `Publish`: embed `provider.OutpostPublisher` (publishes through Outpost's own queue client) unless the broker needs something else.
   - `Sample`: backlog (ready), in-flight (delivered, not settled) and dead-letter depth; `-1` for what the broker can't tell.
   - `ReadDLQ`: drain the dead-letter queue and return the bodies.
   - `Registration.PassEnv` (optional): environment variables the worker inherits for ambient credentials (`AWS_PROFILE`, `GOOGLE_APPLICATION_CREDENTIALS`, ...). Nothing else from mqcheck's environment reaches the worker.
   - `Registration.Attempt` (optional, runs in the worker): the broker's delivery attempt for a received message, from `mqs.Message.QueueMessage` (e.g. through gocloud's `As`). Return 0 when unknown; attempt checks then use handler invocations.
4. **Caps**, from the broker's documentation, with links in the file's comment:
   - `Broker`: what runs (emulator, real service, region).
   - `InFlight`: `exact`, `approximate` or `none`. Without a live in-flight count, held-in-client checks are not observable.
   - `MaxMessageBytes`, `MinVisibilityTimeout`, `MinMaxAttempts`, `SampleEvery`.
   - `Unobservable`: behavior classes this setup can't show (`flow-control`, `lease-timing`, `capacity`, `dead-letter`) and why. Emulators usually list some; such cases report NOT-OBSERVABLE, never PASS.
   - `CapacityNotJudged`: why capacity numbers are recorded but not judged against targets (an emulation with its own throughput). The envelope then reads MEASURED.
   - `ByDesign`: `"<case>/<check>"` → the documented broker behavior that fails that check whatever the consumer does (e.g. a returned message counts a delivery attempt). Only broker semantics belong here, never implementation gaps.
   - `Notes`: anything a reader of the report needs.
5. **Local broker**: a service in `cmd/mqcheck/compose.yml` (image pinned in `.env.test`, a port that doesn't clash with `make up` or `make up/test`, a healthcheck) and the default endpoint pointing at it.
6. **Prove it**: `mqcheck doctor --provider <name>`, then `mqcheck validate --provider <name>` on main. Known consumer gaps on main should show up as FAILs (if a broker shows none, check the provider before trusting it). `mqcheck sweep --provider <name> --older-than 0s` leaves nothing.
7. **Docs**: a row in the README's provider table.

## Skeleton

```go
package providers

func init() {
	provider.Register(provider.Registration{
		Name:     "mybroker",
		Summary:  "MyBroker: local container from `make up`, or a real cluster by config",
		Settings: []provider.Setting{{Env: "MQCHECK_MYBROKER_URL", Default: "mybroker://localhost:4xxxx", Description: "Broker URL."}},
		Open: func(ctx context.Context, cfg provider.Config) (provider.Provider, error) {
			p := &myProvider{url: cfg.Get("MQCHECK_MYBROKER_URL")}
			// connect, check reachability
			p.OutpostPublisher.Env = p.WorkerEnv
			return p, nil
		},
	})
}

type myProvider struct {
	provider.OutpostPublisher
	url string
}

func (p *myProvider) Caps() provider.Caps { /* documented semantics */ }
func (p *myProvider) Provision(ctx context.Context, s provider.QueueSpec) (*provider.Target, error) { /* mqinfra Declare */ }
func (p *myProvider) Teardown(ctx context.Context, t *provider.Target) error { /* mqinfra TearDown */ }
func (p *myProvider) Sweep(ctx context.Context, prefix string, olderThan time.Duration) (int, error) { /* list + ShouldSweep */ }
func (p *myProvider) WorkerEnv(t *provider.Target) map[string]string { /* MYBROKER_* Outpost env */ }
func (p *myProvider) Sample(ctx context.Context, t *provider.Target) (provider.Sample, error) { /* admin API */ }
func (p *myProvider) ReadDLQ(ctx context.Context, t *provider.Target) ([][]byte, error) { /* drain */ }
func (p *myProvider) Close() error { p.OutpostPublisher.Close(); return nil }
```

## If the broker isn't in Outpost's config yet

`WorkerEnv` needs Outpost settings for the broker, so a provider for a new broker lands with (or after) the change that adds the broker to Outpost's configuration. The provider doesn't need anything else from that change.
