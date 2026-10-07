package destgcppubsub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/workloadidentity"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google/externalaccount"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	AuthMethodServiceAccountKey = "service_account_key"
	AuthMethodWorkloadIdentity  = "workload_identity"

	defaultSTSTokenURL        = "https://sts.googleapis.com/v1/token"
	defaultIAMCredentialsURL  = "https://iamcredentials.googleapis.com"
	cloudPlatformScope        = "https://www.googleapis.com/auth/cloud-platform"
	jwtSubjectTokenType       = "urn:ietf:params:oauth:token-type:jwt"
	workloadIdentityAudPrefix = "//iam.googleapis.com/"
	// Google limits the mapped google.subject attribute to 127 bytes.
	maxSubjectBytes = 127
)

// TokenIssuer mints the OIDC token a workload identity pool exchanges for
// Google credentials.
type TokenIssuer interface {
	Mint(subject, audience string) (string, error)
}

type GCPPubSubDestination struct {
	*destregistry.BaseProvider

	tokenIssuer       TokenIssuer
	stsTokenURL       string
	iamCredentialsURL string
	tokenHTTPClient   *http.Client
}

type GCPPubSubDestinationConfig struct {
	ProjectID  string
	Topic      string
	Endpoint   string // For emulator support
	AuthMethod string
}

type GCPPubSubDestinationCredentials struct {
	ServiceAccountJSON       string
	WorkloadIdentityProvider string
	ServiceAccountEmail      string
}

var _ destregistry.Provider = (*GCPPubSubDestination)(nil)

type Option func(*GCPPubSubDestination)

// WithWorkloadIdentity enables the workload_identity auth method: tokens
// minted by issuer, with the destination's tenant as subject, are exchanged
// for Google credentials. Without it the auth method is not offered.
func WithWorkloadIdentity(issuer TokenIssuer) Option {
	return func(d *GCPPubSubDestination) {
		d.tokenIssuer = issuer
	}
}

func New(loader metadata.MetadataLoader, basePublisherOpts []destregistry.BasePublisherOption, opts ...Option) (*GCPPubSubDestination, error) {
	base, err := destregistry.NewBaseProvider(loader, "gcp_pubsub", basePublisherOpts...)
	if err != nil {
		return nil, err
	}

	d := &GCPPubSubDestination{
		BaseProvider:      base,
		stsTokenURL:       defaultSTSTokenURL,
		iamCredentialsURL: defaultIAMCredentialsURL,
		tokenHTTPClient:   &http.Client{Timeout: 10 * time.Second},
	}
	for _, opt := range opts {
		opt(d)
	}
	if d.tokenIssuer == nil {
		base.RemoveFieldOption("auth_method", AuthMethodWorkloadIdentity)
	}
	return d, nil
}

func (d *GCPPubSubDestination) Validate(ctx context.Context, destination *models.Destination) error {
	_, _, err := d.resolveMetadata(ctx, destination)
	if err != nil {
		return err
	}
	return nil
}

func (d *GCPPubSubDestination) CreatePublisher(ctx context.Context, destination *models.Destination) (destregistry.Publisher, error) {
	cfg, creds, err := d.resolveMetadata(ctx, destination)
	if err != nil {
		return nil, destregistry.NewErrDestinationPublishAttempt(err, "gcp_pubsub", map[string]interface{}{
			"error":   "validation_failed",
			"message": err.Error(),
		})
	}

	// Create Pub/Sub client options
	var opts []option.ClientOption

	// Check for emulator endpoint (for testing)
	if cfg.Endpoint != "" {
		opts = append(opts,
			option.WithEndpoint(cfg.Endpoint),
			option.WithoutAuthentication(),
			option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
		)
	} else if cfg.AuthMethod == AuthMethodWorkloadIdentity {
		ts, err := d.workloadIdentityTokenSource(destination.TenantID, creds)
		if err != nil {
			return nil, destregistry.NewErrDestinationPublishAttempt(err, "gcp_pubsub", map[string]interface{}{
				"error":   "client_creation_failed",
				"message": err.Error(),
			})
		}
		opts = append(opts, option.WithTokenSource(ts))
	} else if creds.ServiceAccountJSON != "" {
		// Use service account credentials for production
		opts = append(opts, option.WithCredentialsJSON([]byte(creds.ServiceAccountJSON)))
	}

	// Create the client
	client, err := pubsub.NewClient(ctx, cfg.ProjectID, opts...)
	if err != nil {
		return nil, destregistry.NewErrDestinationPublishAttempt(err, "gcp_pubsub", map[string]interface{}{
			"error":   "client_creation_failed",
			"message": err.Error(),
		})
	}

	// Get the topic
	topic := client.Topic(cfg.Topic)

	return &GCPPubSubPublisher{
		BasePublisher: d.BaseProvider.NewPublisher(destregistry.WithDeliveryMetadata(destination.DeliveryMetadata)),
		client:        client,
		topic:         topic,
		projectID:     cfg.ProjectID,
	}, nil
}

func (d *GCPPubSubDestination) resolveMetadata(ctx context.Context, destination *models.Destination) (*GCPPubSubDestinationConfig, *GCPPubSubDestinationCredentials, error) {
	if err := d.BaseProvider.Validate(ctx, destination); err != nil {
		return nil, nil, err
	}

	authMethod := destination.Config["auth_method"]
	if authMethod == "" {
		authMethod = AuthMethodServiceAccountKey
	}

	if authMethod == AuthMethodWorkloadIdentity &&
		len(workloadidentity.TenantSubject(destination.TenantID)) > maxSubjectBytes {
		return nil, nil, destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
			{
				Field: "tenant_id",
				Type:  "maxlength",
			},
		})
	}

	// Validate service_account_json is valid JSON (if not using emulator endpoint)
	serviceAccountJSON := destination.Credentials["service_account_json"]
	endpoint := destination.Config["endpoint"]

	// Only validate JSON if we're not using an emulator endpoint and service_account_json is provided
	if endpoint == "" && serviceAccountJSON != "" {
		var jsonCheck map[string]interface{}
		if err := json.Unmarshal([]byte(serviceAccountJSON), &jsonCheck); err != nil {
			return nil, nil, destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
				{
					Field: "credentials.service_account_json",
					Type:  "format",
				},
			})
		}
		// Validate required GCP credential fields
		if _, ok := jsonCheck["type"]; !ok {
			return nil, nil, destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
				{
					Field: "credentials.service_account_json",
					Type:  "missing_type",
				},
			})
		}
	}

	return &GCPPubSubDestinationConfig{
			ProjectID:  destination.Config["project_id"],
			Topic:      destination.Config["topic"],
			Endpoint:   destination.Config["endpoint"], // For testing
			AuthMethod: authMethod,
		}, &GCPPubSubDestinationCredentials{
			ServiceAccountJSON:       destination.Credentials["service_account_json"],
			WorkloadIdentityProvider: destination.Credentials["workload_identity_provider"],
			ServiceAccountEmail:      destination.Credentials["service_account_email"],
		}, nil
}

// workloadIdentityTokenSource exchanges tokens minted for the tenant at
// Google STS, then impersonates the service account if one is set.
func (d *GCPPubSubDestination) workloadIdentityTokenSource(tenantID string, creds *GCPPubSubDestinationCredentials) (oauth2.TokenSource, error) {
	if d.tokenIssuer == nil {
		return nil, errors.New("workload identity is not enabled")
	}
	audience := workloadIdentityAudPrefix + strings.TrimPrefix(creds.WorkloadIdentityProvider, workloadIdentityAudPrefix)
	conf := externalaccount.Config{
		Audience:         audience,
		SubjectTokenType: jwtSubjectTokenType,
		TokenURL:         d.stsTokenURL,
		Scopes:           []string{cloudPlatformScope},
		SubjectTokenSupplier: subjectTokenSupplier{
			issuer:   d.tokenIssuer,
			subject:  workloadidentity.TenantSubject(tenantID),
			audience: audience,
		},
	}
	if creds.ServiceAccountEmail != "" {
		conf.ServiceAccountImpersonationURL = fmt.Sprintf("%s/v1/projects/-/serviceAccounts/%s:generateAccessToken",
			d.iamCredentialsURL, url.PathEscape(creds.ServiceAccountEmail))
	}
	// The token source outlives the request that creates the publisher, so it
	// gets its own context; the client bounds each token request.
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, d.tokenHTTPClient)
	return externalaccount.NewTokenSource(ctx, conf)
}

type subjectTokenSupplier struct {
	issuer   TokenIssuer
	subject  string
	audience string
}

func (s subjectTokenSupplier) SubjectToken(ctx context.Context, opts externalaccount.SupplierOptions) (string, error) {
	return s.issuer.Mint(s.subject, s.audience)
}

func (d *GCPPubSubDestination) ComputeTarget(destination *models.Destination) destregistry.DestinationTarget {
	projectID := destination.Config["project_id"]
	topic := destination.Config["topic"]

	return destregistry.DestinationTarget{
		Target:    fmt.Sprintf("%s/%s", projectID, topic),
		TargetURL: fmt.Sprintf("https://console.cloud.google.com/cloudpubsub/topic/detail/%s?project=%s", topic, projectID),
	}
}

func (d *GCPPubSubDestination) Preprocess(newDestination *models.Destination, originalDestination *models.Destination, opts *destregistry.PreprocessDestinationOpts) error {
	// No preprocessing needed for GCP Pub/Sub
	return nil
}

type GCPPubSubPublisher struct {
	*destregistry.BasePublisher

	client    *pubsub.Client
	topic     *pubsub.Topic
	projectID string
}

func (pub *GCPPubSubPublisher) Format(ctx context.Context, event *models.Event) (*pubsub.Message, error) {
	dataBytes := []byte(event.Data)

	// Create metadata
	metadata := pub.BasePublisher.MakeMetadata(event, time.Now())

	// Convert metadata to Pub/Sub attributes (must be strings)
	attributes := make(map[string]string)
	for k, v := range metadata {
		attributes[k] = v
	}

	return &pubsub.Message{
		Data:       dataBytes,
		Attributes: attributes,
	}, nil
}

func (pub *GCPPubSubPublisher) Publish(ctx context.Context, event *models.Event) (*destregistry.Delivery, error) {
	if err := pub.BasePublisher.StartPublish(); err != nil {
		return nil, err
	}
	defer pub.BasePublisher.FinishPublish()

	// Format the message
	msg, err := pub.Format(ctx, event)
	if err != nil {
		return destregistry.NewFormatError("gcp_pubsub", "", err)
	}

	// Publish the message
	result := pub.topic.Publish(ctx, msg)

	// Wait for the publish to complete
	messageID, err := result.Get(ctx)
	if err != nil {
		return &destregistry.Delivery{
				Status: "failed",
				Code:   "ERR",
				Response: map[string]interface{}{
					"error": err.Error(),
				},
			}, destregistry.NewErrDestinationPublishAttempt(err, "gcp_pubsub", map[string]interface{}{
				"error":   "publish_failed",
				"project": pub.projectID,
				"topic":   pub.topic.ID(),
				"message": err.Error(),
			})
	}

	return &destregistry.Delivery{
		Status: "success",
		Code:   "OK",
		Response: map[string]interface{}{
			"message_id": messageID,
			"topic":      pub.topic.ID(),
			"project":    pub.projectID,
		},
	}, nil
}

func (pub *GCPPubSubPublisher) Close() error {
	pub.BasePublisher.StartClose()

	if pub.topic != nil {
		pub.topic.Stop()
	}
	if pub.client != nil {
		return pub.client.Close()
	}

	return nil
}
