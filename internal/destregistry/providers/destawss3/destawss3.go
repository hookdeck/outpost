package destawss3

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awscreds "github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/jmespath/go-jmespath"
)

// Default template that generates keys with timestamp and event ID
// Using join to concatenate the timestamp, underscore, event-id, and .json extension
const defaultKeyTemplate = `join('', [time.rfc3339_nano, '_', metadata."event-id", '.json'])`

// AWSS3Config is the configuration for an S3 destination
type AWSS3Config struct {
	Bucket       string
	Region       string
	KeyTemplate  string // JMESPath expression for generating S3 keys
	StorageClass string
	Endpoint     string // Optional endpoint for testing
}


// AWSS3Credentials is the credentials for an S3 destination
type AWSS3Credentials struct {
	Key     string
	Secret  string
	Session string
}

// AWSS3Provider is the S3 Provider implementation
type AWSS3Provider struct {
	*destregistry.BaseProvider
}

var _ destregistry.Provider = (*AWSS3Provider)(nil)

// New creates a new AWSS3Provider
func New(loader metadata.MetadataLoader) (*AWSS3Provider, error) {
	base, err := destregistry.NewBaseProvider(loader, "aws_s3")
	if err != nil {
		return nil, err
	}

	return &AWSS3Provider{BaseProvider: base}, nil
}

// Validate checks if the destination configuration is valid
func (p *AWSS3Provider) Validate(ctx context.Context, destination *models.Destination) error {
	_, _, err := p.resolveConfig(ctx, destination)
	return err
}

// createS3Client creates a new S3 client using the provided configuration and credentials.
func (p *AWSS3Provider) createS3Client(ctx context.Context, cfg *AWSS3Config, creds *AWSS3Credentials) (*s3.Client, error) {
	sdkConfig, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithCredentialsProvider(awscreds.NewStaticCredentialsProvider(
			creds.Key,
			creds.Secret,
			creds.Session,
		)),
		awsconfig.WithRegion(cfg.Region),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	s3Options := []func(*s3.Options){}
	if cfg.Endpoint != "" {
		s3Options = append(s3Options, func(o *s3.Options) {
			o.BaseEndpoint = awssdk.String(cfg.Endpoint)
			o.UsePathStyle = true // Required for LocalStack
		})
	}

	return s3.NewFromConfig(sdkConfig, s3Options...), nil
}

// CreatePublisher creates a new S3 publisher for the given destination
func (p *AWSS3Provider) CreatePublisher(ctx context.Context, destination *models.Destination) (destregistry.Publisher, error) {
	cfg, creds, err := p.resolveConfig(ctx, destination)
	if err != nil {
		return nil, err
	}

	client, err := p.createS3Client(ctx, cfg, creds)
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 client: %w", err)
	}

	return NewAWSS3Publisher(
		client,
		cfg.Bucket,
		cfg.KeyTemplate,
		cfg.StorageClass,
	), nil
}

// resolveConfig resolves the configuration and credentials for the S3 destination
func (p *AWSS3Provider) resolveConfig(ctx context.Context, destination *models.Destination) (*AWSS3Config, *AWSS3Credentials, error) {
	if err := p.BaseProvider.Validate(ctx, destination); err != nil {
		return nil, nil, err
	}

	sc := destination.Config["storage_class"]
	_, err := parseStorageClass(sc)
	if err != nil {
		return nil, nil, destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
			{
				Field: "config.storage_class",
				Type:  "enum",
			},
		})
	}

	// Use custom template if provided, otherwise use default
	keyTemplate := destination.Config["key_template"]
	if keyTemplate == "" {
		keyTemplate = defaultKeyTemplate
	}

	// Validate the JMESPath expression by compiling it
	if _, err := jmespath.Compile(keyTemplate); err != nil {
		return nil, nil, destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
			{
				Field: "config.key_template",
				Type:  "pattern",
			},
		})
	}

	return &AWSS3Config{
			Bucket:       destination.Config["bucket"],
			Region:       destination.Config["region"],
			KeyTemplate:  keyTemplate,
			StorageClass: sc,
			Endpoint:     destination.Config["endpoint"],
		}, &AWSS3Credentials{
			Key:     destination.Credentials["key"],
			Secret:  destination.Credentials["secret"],
			Session: destination.Credentials["session"],
		}, nil
}

// ComputeTarget returns a human-readable target description for the S3 destination
func (p *AWSS3Provider) ComputeTarget(destination *models.Destination) destregistry.DestinationTarget {
	bucket := destination.Config["bucket"]
	region := destination.Config["region"]
	return destregistry.DestinationTarget{
		Target:    fmt.Sprintf("%s in %s", bucket, region),
		TargetURL: fmt.Sprintf("https://s3.console.aws.amazon.com/s3/buckets/%s?region=%s", bucket, region),
	}
}

// Publisher implementation

// AWSS3Publisher is the S3 publisher implementation
type AWSS3Publisher struct {
	*destregistry.BasePublisher
	client       *s3.Client
	bucket       string
	keyTemplate  *jmespath.JMESPath
	storageClass string
}

func (p *AWSS3Publisher) Close() error {
	p.BasePublisher.StartClose()
	return nil
}

// parseTimeFields converts an event time to pre-parsed time components
func parseTimeFields(t time.Time) map[string]interface{} {
	utc := t.UTC()
	return map[string]interface{}{
		"year":         fmt.Sprintf("%04d", utc.Year()),
		"month":        fmt.Sprintf("%02d", utc.Month()),
		"day":          fmt.Sprintf("%02d", utc.Day()),
		"hour":         fmt.Sprintf("%02d", utc.Hour()),
		"minute":       fmt.Sprintf("%02d", utc.Minute()),
		"second":       fmt.Sprintf("%02d", utc.Second()),
		"date":         utc.Format("2006-01-02"),
		"datetime":     utc.Format("2006-01-02T15:04:05"),
		"unix":         fmt.Sprintf("%d", utc.Unix()),
		"rfc3339":      utc.Format(time.RFC3339),
		"rfc3339_nano": utc.Format(time.RFC3339Nano),
	}
}

func (p *AWSS3Publisher) makeKey(event *models.Event, metadata map[string]string) (string, error) {
	// Convert event data to map[string]interface{}
	dataMap := make(map[string]interface{})
	for k, v := range event.Data {
		dataMap[k] = v
	}

	// Convert metadata to map[string]interface{} for JMESPath
	metadataMap := make(map[string]interface{})
	for k, v := range metadata {
		metadataMap[k] = v
	}

	// Build the data structure for JMESPath evaluation
	templateData := map[string]interface{}{
		"data":     dataMap,
		"metadata": metadataMap,
		"time":     parseTimeFields(event.Time),
	}

	// Evaluate the JMESPath expression
	result, err := p.keyTemplate.Search(templateData)
	if err != nil {
		return "", fmt.Errorf("failed to evaluate key template: %w", err)
	}

	// Convert result to string
	var key string
	switch v := result.(type) {
	case string:
		key = v
	case float64:
		key = fmt.Sprintf("%g", v)
	case int:
		key = fmt.Sprintf("%d", v)
	case bool:
		key = fmt.Sprintf("%t", v)
	default:
		// For complex types or nil, try to convert to string
		if v == nil {
			return "", fmt.Errorf("key template produced nil result")
		}
		key = fmt.Sprintf("%v", v)
	}

	if key == "" {
		return "", fmt.Errorf("key template produced empty string")
	}

	return key, nil
}

func (p *AWSS3Publisher) getStorageClass() (types.StorageClass, error) {
	return parseStorageClass(p.storageClass)
}

func (p *AWSS3Publisher) getChecksums(payload []byte) (string, error) {
	checksum := sha256.Sum256(payload)
	return base64.StdEncoding.EncodeToString(checksum[:]), nil
}

func (p *AWSS3Publisher) Format(_ context.Context, event *models.Event) (*s3.PutObjectInput, error) {
	data, err := json.Marshal(event.Data)
	if err != nil {
		return nil, err
	}

	// Get merged metadata (system + event metadata)
	metadata := p.BasePublisher.MakeMetadata(event, time.Now())

	key, err := p.makeKey(event, metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 key: %w", err)
	}

	storageClass, err := p.getStorageClass()
	if err != nil {
		return nil, fmt.Errorf("failed to get S3 storage class: %w", err)
	}

	checksumSha256, err := p.getChecksums(data)
	if err != nil {
		return nil, fmt.Errorf("failed to compute checksum: %w", err)
	}

	return &s3.PutObjectInput{
		Bucket:            awssdk.String(p.bucket),
		Key:               awssdk.String(key),
		Body:              bytes.NewReader(data),
		Metadata:          metadata,
		StorageClass:      storageClass,
		ContentType:       awssdk.String("application/json"),
		ChecksumSHA256:    awssdk.String(checksumSha256),
		ChecksumAlgorithm: types.ChecksumAlgorithmSha256,
	}, nil
}

func (p *AWSS3Publisher) Publish(ctx context.Context, event *models.Event) (*destregistry.Delivery, error) {
	if err := p.BasePublisher.StartPublish(); err != nil {
		return nil, err
	}
	defer p.BasePublisher.FinishPublish()

	input, err := p.Format(ctx, event)
	if err != nil {
		return nil, err
	}

	_, err = p.client.PutObject(ctx, input)
	if err != nil {
		return &destregistry.Delivery{
				Status: "failed",
				Code:   "ERR",
				Response: map[string]interface{}{
					"error": err.Error(),
				},
			}, destregistry.NewErrDestinationPublishAttempt(err, "aws_s3", map[string]interface{}{
				"error": err.Error(),
			})
	}

	return &destregistry.Delivery{
		Status: "success",
		Code:   "OK",
		Response: map[string]interface{}{
			"bucket": p.bucket,
			"key":    *input.Key,
		},
	}, nil
}

func parseStorageClass(storageClass string) (types.StorageClass, error) {
	if storageClass == "" {
		return types.StorageClassStandard, nil
	}

	// Just here so we can call .Values() on the types.ObjectStorageClass type
	var sc types.StorageClass

	for _, val := range sc.Values() {
		if strings.EqualFold(string(val), storageClass) {
			return val, nil
		}
	}

	return "", fmt.Errorf("invalid S3 storage class: %q", storageClass)
}

// NewAWSS3Publisher exposed for testing
func NewAWSS3Publisher(
	client *s3.Client,
	bucket, keyTemplateStr, storageClass string,
) *AWSS3Publisher {
	// Compile the JMESPath expression (we assume it's already validated)
	tmpl, err := jmespath.Compile(keyTemplateStr)
	if err != nil {
		// This should not happen as template is validated in resolveConfig
		panic(fmt.Sprintf("invalid key template: %v", err))
	}

	return &AWSS3Publisher{
		BasePublisher: &destregistry.BasePublisher{},
		client:        client,
		bucket:        bucket,
		keyTemplate:   tmpl,
		storageClass:  storageClass,
	}
}
