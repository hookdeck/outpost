package destazureservicebus

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/models"
)

type AzureServiceBusDestination struct {
	*destregistry.BaseProvider
}

type AzureServiceBusDestinationConfig struct {
	Name string
}

type AzureServiceBusDestinationCredentials struct {
	ConnectionString string
}

var _ destregistry.Provider = (*AzureServiceBusDestination)(nil)

func New(loader metadata.MetadataLoader) (*AzureServiceBusDestination, error) {
	base, err := destregistry.NewBaseProvider(loader, "azure_servicebus")
	if err != nil {
		return nil, err
	}

	return &AzureServiceBusDestination{
		BaseProvider: base,
	}, nil
}

func (d *AzureServiceBusDestination) Validate(ctx context.Context, destination *models.Destination) error {
	// Just use base validation - let Azure SDK handle connection string validation at runtime
	return d.BaseProvider.Validate(ctx, destination)
}

func (d *AzureServiceBusDestination) CreatePublisher(ctx context.Context, destination *models.Destination) (destregistry.Publisher, error) {
	cfg, creds, err := d.resolveMetadata(ctx, destination)
	if err != nil {
		return nil, err
	}

	return &AzureServiceBusPublisher{
		BasePublisher:    &destregistry.BasePublisher{},
		connectionString: creds.ConnectionString,
		queueOrTopic:     cfg.Name,
	}, nil
}

func (d *AzureServiceBusDestination) ComputeTarget(destination *models.Destination) destregistry.DestinationTarget {
	name, ok := destination.Config["name"]
	if !ok {
		return destregistry.DestinationTarget{}
	}

	// Try to extract namespace from connection string
	if connStr, ok := destination.Credentials["connection_string"]; ok {
		namespace := parseNamespaceFromConnectionString(connStr)
		if namespace != "" {
			return destregistry.DestinationTarget{
				Target:    fmt.Sprintf("%s/%s", namespace, name),
				TargetURL: "",
			}
		}
	}

	// Fallback to just the name if we can't parse namespace
	return destregistry.DestinationTarget{
		Target:    name,
		TargetURL: "",
	}
}

func (d *AzureServiceBusDestination) Preprocess(newDestination *models.Destination, originalDestination *models.Destination, opts *destregistry.PreprocessDestinationOpts) error {
	// No preprocessing needed for Azure Service Bus
	return nil
}

func (d *AzureServiceBusDestination) resolveMetadata(ctx context.Context, destination *models.Destination) (*AzureServiceBusDestinationConfig, *AzureServiceBusDestinationCredentials, error) {
	if err := d.BaseProvider.Validate(ctx, destination); err != nil {
		return nil, nil, err
	}

	return &AzureServiceBusDestinationConfig{
			Name: destination.Config["name"],
		}, &AzureServiceBusDestinationCredentials{
			ConnectionString: destination.Credentials["connection_string"],
		}, nil
}

type AzureServiceBusPublisher struct {
	*destregistry.BasePublisher
	connectionString string
	queueOrTopic     string
	client           *azservicebus.Client
	sender           *azservicebus.Sender
}

func (p *AzureServiceBusPublisher) ensureSender() (*azservicebus.Sender, error) {
	if p.client == nil {
		client, err := azservicebus.NewClientFromConnectionString(p.connectionString, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create Azure Service Bus client: %w", err)
		}
		p.client = client
	}

	if p.sender == nil {
		sender, err := p.client.NewSender(p.queueOrTopic, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create sender for queue or topic %s: %w", p.queueOrTopic, err)
		}
		p.sender = sender
	}

	return p.sender, nil
}

func (p *AzureServiceBusPublisher) Format(ctx context.Context, event *models.Event) (*azservicebus.Message, error) {
	dataBytes, err := json.Marshal(event.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event data: %w", err)
	}

	messageMetadata := map[string]any{}
	metadata := p.BasePublisher.MakeMetadata(event, time.Now())
	for k, v := range metadata {
		messageMetadata[k] = v
	}

	message := &azservicebus.Message{
		Body:                  dataBytes,
		ApplicationProperties: messageMetadata,
	}

	return message, nil
}

func (p *AzureServiceBusPublisher) Publish(ctx context.Context, event *models.Event) (*destregistry.Delivery, error) {
	if err := p.BasePublisher.StartPublish(); err != nil {
		return nil, err
	}
	defer p.BasePublisher.FinishPublish()

	message, err := p.Format(ctx, event)
	if err != nil {
		return nil, err
	}

	sender, err := p.ensureSender()
	if err != nil {
		return nil, err
	}

	if err := sender.SendMessage(ctx, message, nil); err != nil {
		return &destregistry.Delivery{
				Status: "failed",
				Code:   "ERR",
				Response: map[string]interface{}{
					"error": err.Error(),
				},
			}, destregistry.NewErrDestinationPublishAttempt(err, "azure_servicebus", map[string]interface{}{
				"error": err.Error(),
			})
	}

	return &destregistry.Delivery{
		Status:   "success",
		Code:     "OK",
		Response: map[string]interface{}{},
	}, nil
}

func (p *AzureServiceBusPublisher) Close() error {
	p.BasePublisher.StartClose()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if p.sender != nil {
		if err := p.sender.Close(ctx); err != nil {
			return err
		}
	}

	if p.client != nil {
		if err := p.client.Close(ctx); err != nil {
			return err
		}
	}

	return nil
}

// parseNamespaceFromConnectionString extracts the namespace from an Azure Service Bus connection string.
// Connection strings typically have the format:
// Endpoint=sb://namespace.servicebus.windows.net/;SharedAccessKeyName=...;SharedAccessKey=...
func parseNamespaceFromConnectionString(connStr string) string {
	// Split by semicolons to get individual components
	parts := strings.Split(connStr, ";")
	for _, part := range parts {
		if strings.HasPrefix(part, "Endpoint=") {
			endpoint := strings.TrimPrefix(part, "Endpoint=")
			// Remove protocol prefix
			endpoint = strings.TrimPrefix(endpoint, "sb://")
			// Extract namespace (everything before first dot)
			if idx := strings.Index(endpoint, "."); idx > 0 {
				return endpoint[:idx]
			}
		}
	}
	return ""
}
