package providers

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/hookdeck/outpost/internal/mqcheck/provider"
	"github.com/hookdeck/outpost/internal/mqinfra"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/awsutil"
)

// AWS SQS, on LocalStack (default, `make up`) or a real account.
//
// Broker semantics used here (https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/):
//   - per-message visibility timeout; an unsettled message becomes visible
//     again when it expires;
//   - ApproximateReceiveCount counts every receive, including one after a
//     message was returned (ChangeMessageVisibility 0);
//   - redrive policy dead-letters after maxReceiveCount receives;
//   - ApproximateNumberOfMessagesNotVisible is the in-flight count (exact on
//     LocalStack, approximate on SQS);
//   - standard queues may deliver a message more than once.

const (
	sqsEnvEndpoint = "MQCHECK_SQS_ENDPOINT"
	sqsEnvRegion   = "MQCHECK_SQS_REGION"
	sqsEnvKeyID    = "MQCHECK_SQS_ACCESS_KEY_ID"
	sqsEnvSecret   = "MQCHECK_SQS_SECRET_ACCESS_KEY"
)

func init() {
	provider.Register(provider.Registration{
		Name:    "awssqs",
		Summary: "AWS SQS: LocalStack from `make up` by default, or a real account by config",
		Settings: []provider.Setting{
			{Env: sqsEnvEndpoint, Default: "http://localhost:44566", Description: `SQS endpoint. "aws" uses the AWS endpoint for the region (real SQS).`},
			{Env: sqsEnvRegion, Default: "us-east-1", Description: "AWS region."},
			{Env: sqsEnvKeyID, Description: `Access key id. LocalStack: defaults to "test". Real SQS: unset uses the AWS SDK default credential chain.`},
			{Env: sqsEnvSecret, Secret: true, Description: "Secret access key, set together with the key id."},
		},
		PassEnv: []string{"AWS_PROFILE", "AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"},
		Open:    openSQS,
		Attempt: sqsAttempt,
	})
}

type sqsProvider struct {
	provider.OutpostPublisher
	endpoint string // "" = AWS
	region   string
	creds    string // "key:secret:" or "" for the default chain
	client   *sqs.Client
}

func openSQS(ctx context.Context, cfg provider.Config) (provider.Provider, error) {
	p := &sqsProvider{endpoint: cfg.Get(sqsEnvEndpoint), region: cfg.Get(sqsEnvRegion)}
	if p.endpoint == "aws" {
		p.endpoint = ""
	}
	key, secret := cfg.Get(sqsEnvKeyID), cfg.Get(sqsEnvSecret)
	if p.local() && key == "" && secret == "" {
		key, secret = "test", "test"
	}
	if (key == "") != (secret == "") {
		return nil, fmt.Errorf("%s and %s must be set together", sqsEnvKeyID, sqsEnvSecret)
	}
	if key != "" {
		p.creds = key + ":" + secret + ":"
	}
	client, err := awsutil.SQSClientFromConfig(ctx, &mqs.AWSSQSConfig{Endpoint: p.endpoint, Region: p.region, ServiceAccountCredentials: p.creds})
	if err != nil {
		return nil, err
	}
	p.client = client
	if _, err := client.ListQueues(ctx, &sqs.ListQueuesInput{MaxResults: aws.Int32(1)}); err != nil {
		return nil, fmt.Errorf("reach SQS at %s: %w", p.describeEndpoint(), err)
	}
	p.OutpostPublisher.Env = p.WorkerEnv
	return p, nil
}

func (p *sqsProvider) local() bool { return p.endpoint != "" }

func (p *sqsProvider) describeEndpoint() string {
	if p.local() {
		return p.endpoint
	}
	return "AWS " + p.region
}

func (p *sqsProvider) Caps() provider.Caps {
	c := provider.Caps{
		MinMaxAttempts: 1,
		ByDesign: map[string]string{
			"C12.1/no-extra-attempt-on-return": "SQS counts every receive: a message returned to the queue (visibility set to 0) is counted again when it is next received.",
		},
		Unobservable: map[provider.Class]string{},
	}
	if p.local() {
		c.Broker = "LocalStack (SQS API) at " + p.endpoint
		c.InFlight = provider.AccuracyExact
		c.MaxMessageBytes = 256 << 10
		c.SampleEvery = 250 * time.Millisecond
		c.CapacityNotJudged = "LocalStack's throughput and latency are its own, not SQS's: compare runs, don't judge against targets"
		c.Notes = []string{
			"LocalStack emulates SQS: visibility timeout, receive count and redrive follow the SQS API; timing and throughput are LocalStack's, not SQS's.",
		}
	} else {
		c.Broker = "AWS SQS " + p.region
		c.InFlight = provider.AccuracyApproximate
		c.MaxMessageBytes = 1 << 20
		c.SampleEvery = time.Second
		c.Notes = []string{"Standard queues may deliver a message twice (broker duplicates are reported, not failed)."}
	}
	return c
}

func (p *sqsProvider) infra(t *provider.Target) *mqinfra.MQInfraConfig {
	return &mqinfra.MQInfraConfig{
		AWSSQS: &mqinfra.AWSSQSInfraConfig{
			Endpoint:                  p.endpoint,
			Region:                    p.region,
			ServiceAccountCredentials: p.creds,
			Topic:                     t.Spec.Name,
			DLQ:                       t.Spec.Name + "-dlq",
		},
		Policy: mqinfra.Policy{
			VisibilityTimeout: int(t.Spec.VisibilityTimeout / time.Second),
			RetryLimit:        t.Spec.MaxAttempts - 1,
		},
	}
}

// Provision declares the queue and its dead-letter queue with Outpost's own
// infrastructure code.
func (p *sqsProvider) Provision(ctx context.Context, spec provider.QueueSpec) (*provider.Target, error) {
	t := &provider.Target{Spec: spec, Data: map[string]string{}}
	if err := mqinfra.New(p.infra(t)).Declare(ctx); err != nil {
		return nil, err
	}
	for _, q := range []struct{ key, name string }{{"url", spec.Name}, {"dlq_url", spec.Name + "-dlq"}} {
		u, err := awsutil.RetrieveQueueURL(ctx, p.client, q.name)
		if err != nil {
			return nil, err
		}
		t.Data[q.key] = u
	}
	return t, nil
}

func (p *sqsProvider) Teardown(ctx context.Context, t *provider.Target) error {
	var errs []error
	for _, k := range []string{"url", "dlq_url"} {
		if u := t.Data[k]; u != "" {
			if err := awsutil.DeleteQueue(ctx, p.client, u); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// Sweep deletes queues made under prefix. Creation time comes from the queue
// name (see provider.ShouldSweep).
func (p *sqsProvider) Sweep(ctx context.Context, prefix string, olderThan time.Duration) (int, error) {
	n := 0
	pager := sqs.NewListQueuesPaginator(p.client, &sqs.ListQueuesInput{QueueNamePrefix: aws.String(prefix + "-"), MaxResults: aws.Int32(1000)})
	for pager.HasMorePages() {
		out, err := pager.NextPage(ctx)
		if err != nil {
			return n, err
		}
		for _, u := range out.QueueUrls {
			if !provider.ShouldSweep(path.Base(u), prefix, olderThan) {
				continue
			}
			if err := awsutil.DeleteQueue(ctx, p.client, u); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, nil
}

// WorkerEnv is how an operator points Outpost's delivery queue at the queue.
func (p *sqsProvider) WorkerEnv(t *provider.Target) map[string]string {
	env := map[string]string{
		"AWS_SQS_REGION":         p.region,
		"AWS_SQS_DELIVERY_QUEUE": t.Spec.Name,
		"AWS_SQS_DELIVERY_DLQ":   t.Spec.Name + "-dlq",
	}
	if p.endpoint != "" {
		env["AWS_SQS_ENDPOINT"] = p.endpoint
	}
	if key, secret, ok := strings.Cut(strings.TrimSuffix(p.creds, ":"), ":"); ok {
		env["AWS_SQS_ACCESS_KEY_ID"] = key
		env["AWS_SQS_SECRET_ACCESS_KEY"] = secret
	}
	return env
}

func (p *sqsProvider) Sample(ctx context.Context, t *provider.Target) (provider.Sample, error) {
	s := provider.Sample{At: time.Now(), Ready: -1, InFlight: -1, DLQ: -1}
	main, err := p.counts(ctx, t.Data["url"])
	if err != nil {
		return s, err
	}
	s.Ready = main[sqstypes.QueueAttributeNameApproximateNumberOfMessages]
	s.InFlight = main[sqstypes.QueueAttributeNameApproximateNumberOfMessagesNotVisible]
	if dlq, err := p.counts(ctx, t.Data["dlq_url"]); err == nil {
		s.DLQ = dlq[sqstypes.QueueAttributeNameApproximateNumberOfMessages] + dlq[sqstypes.QueueAttributeNameApproximateNumberOfMessagesNotVisible]
	}
	return s, nil
}

func (p *sqsProvider) counts(ctx context.Context, url string) (map[sqstypes.QueueAttributeName]int64, error) {
	names := []sqstypes.QueueAttributeName{
		sqstypes.QueueAttributeNameApproximateNumberOfMessages,
		sqstypes.QueueAttributeNameApproximateNumberOfMessagesNotVisible,
	}
	out, err := p.client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{QueueUrl: aws.String(url), AttributeNames: names})
	if err != nil {
		return nil, err
	}
	m := map[sqstypes.QueueAttributeName]int64{}
	for _, n := range names {
		v, _ := strconv.ParseInt(out.Attributes[string(n)], 10, 64)
		m[n] = v
	}
	return m, nil
}

func (p *sqsProvider) ReadDLQ(ctx context.Context, t *provider.Target) ([][]byte, error) {
	var bodies [][]byte
	for empty := 0; empty < 2; {
		out, err := p.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl: aws.String(t.Data["dlq_url"]), MaxNumberOfMessages: 10, WaitTimeSeconds: 1,
		})
		if err != nil {
			return bodies, err
		}
		if len(out.Messages) == 0 {
			empty++
			continue
		}
		for _, m := range out.Messages {
			bodies = append(bodies, []byte(aws.ToString(m.Body)))
			_, _ = p.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{QueueUrl: aws.String(t.Data["dlq_url"]), ReceiptHandle: m.ReceiptHandle})
		}
	}
	return bodies, nil
}

func (p *sqsProvider) Close() error {
	p.OutpostPublisher.Close()
	return nil
}

// sqsAttempt reads ApproximateReceiveCount from a message received through
// gocloud's SQS driver.
func sqsAttempt(queueMessage any) int {
	as, ok := queueMessage.(interface{ As(any) bool })
	if !ok {
		return 0
	}
	var m sqstypes.Message
	if !as.As(&m) {
		return 0
	}
	n, _ := strconv.Atoi(m.Attributes[string(sqstypes.MessageSystemAttributeNameApproximateReceiveCount)])
	return n
}
