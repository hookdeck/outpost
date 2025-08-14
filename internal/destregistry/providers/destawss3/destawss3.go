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
)

// AWSS3Config is the configuration for an S3 destination
type AWSS3Config struct {
	Bucket           string
	Region           string
	KeyPrefix        string
	KeySuffix        string
	IncludeTimestamp bool
	IncludeEventID   bool
	StorageClass     string
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

	return s3.NewFromConfig(sdkConfig), nil
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
		cfg.KeyPrefix,
		cfg.KeySuffix,
		cfg.StorageClass,
		cfg.IncludeTimestamp,
		cfg.IncludeEventID,
	), nil
}

// resolveConfig resolves the configuration and credentials for the S3 destination
func (p *AWSS3Provider) resolveConfig(ctx context.Context, destination *models.Destination) (*AWSS3Config, *AWSS3Credentials, error) {
	if err := p.BaseProvider.Validate(ctx, destination); err != nil {
		return nil, nil, err
	}

	includeTimestamp := true
	if tsStr, ok := destination.Config["include_timestamp"]; ok {
		includeTimestamp = tsStr == "true" || tsStr == "on"
	}

	includeEventID := true
	if eidStr, ok := destination.Config["include_event_id"]; ok {
		includeEventID = eidStr == "true" || eidStr == "on"
	}

	sc := destination.Config["storage_class"]

	_, err := parseStorageClass(sc)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid storage class %q: %w", sc, err)
	}

	return &AWSS3Config{
			Bucket:           destination.Config["bucket"],
			Region:           destination.Config["region"],
			KeyPrefix:        destination.Config["key_prefix"],
			KeySuffix:        destination.Config["key_suffix"],
			StorageClass:     sc,
			IncludeTimestamp: includeTimestamp,
			IncludeEventID:   includeEventID,
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
		TargetURL: "",
	}
}

// Publisher implementation

// AWSS3Publisher is the S3 publisher implementation
type AWSS3Publisher struct {
	*destregistry.BasePublisher
	client           *s3.Client
	bucket           string
	keyPrefix        string
	keySuffix        string
	includeTimestamp bool
	includeEventID   bool
	storageClass     string
}

func (p *AWSS3Publisher) Close() error {
	p.BasePublisher.StartClose()
	return nil
}

func (p *AWSS3Publisher) makeKey(event *models.Event) (string, error) {
	sb := &strings.Builder{}

	if len(p.keyPrefix) > 0 {
		if _, err := sb.WriteString(p.keyPrefix); err != nil {
			return "", fmt.Errorf("failed to append key prefix: %w", err)
		}
	}

	if p.includeTimestamp {
		if _, err := sb.WriteString(event.Time.UTC().Format(time.RFC3339Nano)); err != nil {
			return "", fmt.Errorf("failed to append timestamp: %w", err)
		}

		if p.includeEventID {
			if _, err := sb.WriteString("_"); err != nil {
				return "", fmt.Errorf("failed to append underscore before event ID: %w", err)
			}
		}
	}

	if p.includeEventID {
		if _, err := sb.WriteString(event.ID); err != nil {
			return "", fmt.Errorf("failed to append event ID: %w", err)
		}
	}

	if len(p.keySuffix) > 0 {
		if _, err := sb.WriteString(p.keySuffix); err != nil {
			return "", fmt.Errorf("failed to append key suffix: %w", err)
		}
	}

	if sb.Len() == 0 {
		return "", fmt.Errorf("makeKey result cannot be empty")
	}

	return sb.String(), nil
}

func (p *AWSS3Publisher) getStorageClass() (types.StorageClass, error) {
	return parseStorageClass(p.storageClass)
}

func (p *AWSS3Publisher) getChecksums(payload []byte) (string, error) {
	hasher := sha256.New()

	if _, err := hasher.Write(payload); err != nil {
		return "", err
	}

	checksum := hasher.Sum(nil)

	return base64.StdEncoding.EncodeToString(checksum), nil
}

func (p *AWSS3Publisher) Format(_ context.Context, event *models.Event) (*s3.PutObjectInput, error) {
	data, err := json.Marshal(event.Data)
	if err != nil {
		return nil, err
	}

	key, err := p.makeKey(event)
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
		Metadata:          event.Metadata,
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
		return nil, destregistry.NewErrDestinationPublishAttempt(
			err,
			"aws_s3",
			map[string]interface{}{"error": "format_failed", "message": err.Error()},
		)
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
	bucket, keyPrefix, keySuffix, storageClass string,
	includeTimestamp, includeEventID bool,
) *AWSS3Publisher {
	return &AWSS3Publisher{
		BasePublisher:    &destregistry.BasePublisher{},
		client:           client,
		bucket:           bucket,
		keyPrefix:        keyPrefix,
		keySuffix:        keySuffix,
		includeTimestamp: includeTimestamp,
		includeEventID:   includeEventID,
		storageClass:     storageClass,
	}
}
