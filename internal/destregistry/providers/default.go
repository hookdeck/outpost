package destregistrydefault

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawseventbridge"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawskinesis"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawss3"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawssqs"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destazureservicebus"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destcfqueues"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destgcppubsub"
	"github.com/hookdeck/outpost/internal/destregistry/providers/desthookdeck"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destkafka"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destmcp"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destrabbitmq"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/emetrics"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/hookdeck/outpost/internal/proxychain"
	"github.com/hookdeck/outpost/internal/topicschema"
)

// WebhookHeaderConfig is the resolved directive for a single webhook system
// header. The config layer collapses the three-state name config (and the
// deprecated DISABLE_* flag) into this: an empty Name with Disabled false means
// "use the default '<prefix>' + key".
type WebhookHeaderConfig struct {
	Name     string
	Disabled bool
}

type DestWebhookConfig struct {
	// MetadataName selects the metadata/providers entry describing the
	// provider; empty means "webhook".
	MetadataName             string
	ProxyURL                 string
	HeaderPrefix             string
	EventIDHeader            WebhookHeaderConfig
	SignatureHeader          WebhookHeaderConfig
	TimestampHeader          WebhookHeaderConfig
	TopicHeader              WebhookHeaderConfig
	TimestampFormat          string
	SignatureContentTemplate string
	SignatureHeaderTemplate  string
	SignatureEncoding        string
	SignatureAlgorithm       string
	SigningSecretTemplate    string
	MaxResponseBodyBytes     int
	SignatureSecretEncoding  string
	SignatureSecretPrefix    string
	Compat                   *destwebhook.CompatSignatureConfig
}

type DestAWSKinesisConfig struct {
	MetadataInPayload bool
}

type DestAWSEventBridgeConfig struct {
	Source string
}

// DestMCPConfig configures the mcp provider. Unlike the other provider
// configs it holds runtime objects, built by the service from MCP_* settings
// (and Redis, for the Verifier), not by the config layer. Zero fields take
// safe defaults; see destmcp.Config.
type DestMCPConfig struct {
	// Catalog is the topic catalog; nil allows no MCP subscription.
	Catalog *topicschema.Catalog
	// Client is the netguard client shared with the Verifier and the
	// Notifier; nil builds one from Guard.
	Client *http.Client
	// Guard is the guard behind Client, for subscribe-time address checks;
	// nil is a strict guard (no allowlist).
	Guard *netguard.Guard
	// HostLimiter is the MCP_MAX_INFLIGHT_PER_HOST limiter shared with the
	// Verifier and the Notifier; nil gives the provider its own of 8.
	HostLimiter *netguard.HostLimiter
	// Verifier verifies callbacks (API service only). Nil makes every
	// subscribe-time validation fail closed. *mcpevents.Verifier implements
	// it; tests can pass a fake.
	Verifier destmcp.Verifier
	// CodeProfile is MCP_ERROR_CODES.
	CodeProfile mcpevents.CodeProfile
	// SecretRotationGrace is MCP_SECRET_ROTATION_GRACE; 0 means 24h.
	SecretRotationGrace time.Duration
	// MaxResponseBodyBytes caps the stored response body; 0 means 4096,
	// negative stores none.
	MaxResponseBodyBytes int
	// UserAgent overrides RegisterDefaultDestinationOptions.UserAgent.
	UserAgent string
}

type RegisterDefaultDestinationOptions struct {
	UserAgent                   string
	IncludeMillisecondTimestamp bool
	// ProxyURL is the forward proxy chain for RabbitMQ and Kafka. Webhooks
	// use Webhook.ProxyURL only; the config layer resolves its fallback to
	// this value.
	ProxyURL       string
	Webhook        *DestWebhookConfig
	AWSKinesis     *DestAWSKinesisConfig
	AWSEventBridge *DestAWSEventBridgeConfig
	// MCP configures the mcp provider, which is always registered (last).
	// Nil gives it an empty catalog, a strict guard and no Verifier: it can
	// deliver to existing subscriptions but validates none.
	MCP *DestMCPConfig

	// DeliveryMaxConcurrency is the delivery worker pool size. It bounds how
	// many deliveries can be in flight, and therefore how many connections a
	// single destination could need. 0 leaves Go's per-host default in place.
	DeliveryMaxConcurrency int
}

// RegisterDefault registers the default destination providers with the registry.
// NOTE: The order of registration will determine the order of the provider array
// returned when listing providers.
func RegisterDefault(registry destregistry.Registry, opts RegisterDefaultDestinationOptions) error {
	loader := registry.MetadataLoader()

	// Build base publisher options that apply to all providers
	basePublisherOpts := []destregistry.BasePublisherOption{}
	if opts.IncludeMillisecondTimestamp {
		basePublisherOpts = append(basePublisherOpts, destregistry.WithMillisecondTimestamp(opts.IncludeMillisecondTimestamp))
	}

	// Webhook destinations fan out across arbitrarily many hosts, so their pool
	// needs breadth as well as depth. The hookdeck provider talks to one host.
	fanOutPool := destregistry.SizeFanOutPool(opts.DeliveryMaxConcurrency)
	singleHostPool := destregistry.SizeSingleHostPool(opts.DeliveryMaxConcurrency)

	emeter, err := emetrics.New()
	if err != nil {
		return err
	}
	connObserver := func(destinationType string) func(bool) {
		return func(reused bool) {
			emeter.DeliveryConnection(context.Background(), reused, destinationType)
		}
	}

	proxy, err := proxychain.Parse(opts.ProxyURL)
	if err != nil {
		return fmt.Errorf("destinations proxy: %w", err)
	}

	var webhookProxy []*url.URL
	if opts.Webhook != nil {
		webhookProxy, err = proxychain.Parse(opts.Webhook.ProxyURL)
		if err != nil {
			return fmt.Errorf("webhook proxy: %w", err)
		}
	}

	webhookOpts := []destwebhook.Option{
		destwebhook.WithUserAgent(opts.UserAgent),
		destwebhook.WithConnectionPool(fanOutPool),
		destwebhook.WithConnectionObserver(connObserver("webhook")),
	}
	if opts.Webhook != nil {
		webhookOpts = append(webhookOpts,
			destwebhook.WithProxy(webhookProxy),
			destwebhook.WithHeaderPrefix(opts.Webhook.HeaderPrefix),
			destwebhook.WithEventIDHeader(opts.Webhook.EventIDHeader.Name, opts.Webhook.EventIDHeader.Disabled),
			destwebhook.WithSignatureHeader(opts.Webhook.SignatureHeader.Name, opts.Webhook.SignatureHeader.Disabled),
			destwebhook.WithTimestampHeader(opts.Webhook.TimestampHeader.Name, opts.Webhook.TimestampHeader.Disabled),
			destwebhook.WithTopicHeader(opts.Webhook.TopicHeader.Name, opts.Webhook.TopicHeader.Disabled),
			destwebhook.WithTimestampFormat(opts.Webhook.TimestampFormat),
			destwebhook.WithSignatureContentTemplate(opts.Webhook.SignatureContentTemplate),
			destwebhook.WithSignatureHeaderTemplate(opts.Webhook.SignatureHeaderTemplate),
			destwebhook.WithSignatureEncoding(opts.Webhook.SignatureEncoding),
			destwebhook.WithSignatureAlgorithm(opts.Webhook.SignatureAlgorithm),
			destwebhook.WithSigningSecretTemplate(opts.Webhook.SigningSecretTemplate),
			destwebhook.WithMaxResponseBodyBytes(opts.Webhook.MaxResponseBodyBytes),
			destwebhook.WithSecretEncoding(opts.Webhook.SignatureSecretEncoding, opts.Webhook.SignatureSecretPrefix),
			destwebhook.WithCompatSignature(opts.Webhook.Compat),
		)
		if opts.Webhook.MetadataName != "" {
			webhookOpts = append(webhookOpts, destwebhook.WithMetadataName(opts.Webhook.MetadataName))
		}
	}
	webhook, err := destwebhook.New(loader, basePublisherOpts, webhookOpts...)
	if err != nil {
		return err
	}
	registry.RegisterProvider("webhook", webhook)

	hookdeck, err := desthookdeck.New(loader, basePublisherOpts,
		desthookdeck.WithUserAgent(opts.UserAgent),
		desthookdeck.WithConnectionPool(singleHostPool),
		desthookdeck.WithConnectionObserver(connObserver("hookdeck")))
	if err != nil {
		return err
	}
	registry.RegisterProvider("hookdeck", hookdeck)

	awsSQS, err := destawssqs.New(loader, basePublisherOpts)
	if err != nil {
		return err
	}
	registry.RegisterProvider("aws_sqs", awsSQS)

	awsEventBridgeOpts := []destawseventbridge.Option{}
	if opts.AWSEventBridge != nil {
		awsEventBridgeOpts = append(awsEventBridgeOpts,
			destawseventbridge.WithSource(opts.AWSEventBridge.Source),
		)
	}
	awsEventBridge, err := destawseventbridge.New(loader, basePublisherOpts, awsEventBridgeOpts...)
	if err != nil {
		return err
	}
	registry.RegisterProvider("aws_eventbridge", awsEventBridge)

	awsKinesisOpts := []destawskinesis.Option{}
	if opts.AWSKinesis != nil {
		awsKinesisOpts = append(awsKinesisOpts,
			destawskinesis.WithMetadataInPayload(opts.AWSKinesis.MetadataInPayload),
		)
	}
	awsKinesis, err := destawskinesis.New(loader, basePublisherOpts, awsKinesisOpts...)
	if err != nil {
		return err
	}
	registry.RegisterProvider("aws_kinesis", awsKinesis)

	awsS3, err := destawss3.New(loader, basePublisherOpts)
	if err != nil {
		return err
	}
	registry.RegisterProvider("aws_s3", awsS3)

	gcpPubSub, err := destgcppubsub.New(loader, basePublisherOpts)
	if err != nil {
		return err
	}
	registry.RegisterProvider("gcp_pubsub", gcpPubSub)

	azureServiceBus, err := destazureservicebus.New(loader, basePublisherOpts)
	if err != nil {
		return err
	}
	registry.RegisterProvider("azure_servicebus", azureServiceBus)

	rabbitmq, err := destrabbitmq.New(loader, basePublisherOpts, destrabbitmq.WithProxy(proxy))
	if err != nil {
		return err
	}
	registry.RegisterProvider("rabbitmq", rabbitmq)

	kafkaDest, err := destkafka.New(loader, basePublisherOpts, destkafka.WithProxy(proxy))
	if err != nil {
		return err
	}
	registry.RegisterProvider("kafka", kafkaDest)

	cloudflareQueues, err := destcfqueues.New(loader, basePublisherOpts,
		destcfqueues.WithUserAgent(opts.UserAgent),
		destcfqueues.WithConnectionPool(singleHostPool),
		destcfqueues.WithConnectionObserver(connObserver("cloudflare_queues")))
	if err != nil {
		return err
	}
	registry.RegisterProvider("cloudflare_queues", cloudflareQueues)

	// mcp is registered last so it lists after every form-based type.
	mcpCfg := destmcp.Config{
		UserAgent: opts.UserAgent,
		// Per-host depth never needs more than the in-flight cap.
		Pool: destregistry.PoolSizing{
			MaxIdleConns:        fanOutPool.MaxIdleConns,
			MaxIdleConnsPerHost: min(fanOutPool.MaxIdleConnsPerHost, destmcp.DefaultMaxInflightPerHost),
		},
		OnConnection: connObserver(destmcp.Type),
	}
	if opts.MCP != nil {
		mcpCfg.Catalog = opts.MCP.Catalog
		mcpCfg.Client = opts.MCP.Client
		mcpCfg.Guard = opts.MCP.Guard
		mcpCfg.HostLimiter = opts.MCP.HostLimiter
		mcpCfg.Verifier = opts.MCP.Verifier
		mcpCfg.CodeProfile = opts.MCP.CodeProfile
		mcpCfg.SecretRotationGrace = opts.MCP.SecretRotationGrace
		mcpCfg.MaxResponseBodyBytes = opts.MCP.MaxResponseBodyBytes
		if opts.MCP.UserAgent != "" {
			mcpCfg.UserAgent = opts.MCP.UserAgent
		}
	}
	mcp, err := destmcp.New(loader, mcpCfg)
	if err != nil {
		return err
	}
	registry.RegisterProvider(destmcp.Type, mcp)

	return nil
}
