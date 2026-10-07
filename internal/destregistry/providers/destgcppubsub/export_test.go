package destgcppubsub

import (
	"context"

	"github.com/hookdeck/outpost/internal/models"
	"golang.org/x/oauth2"
	"google.golang.org/api/option"
)

// WithGoogleEndpoints points the workload identity exchange at fake STS and
// IAM Credentials servers.
func WithGoogleEndpoints(stsTokenURL, iamCredentialsURL string) Option {
	return func(d *GCPPubSubDestination) {
		d.stsTokenURL = stsTokenURL
		d.iamCredentialsURL = iamCredentialsURL
	}
}

// WithClientOptions appends options to the Pub/Sub client, e.g. to point it
// at a fake server.
func WithClientOptions(opts ...option.ClientOption) Option {
	return func(d *GCPPubSubDestination) {
		d.clientOptions = opts
	}
}

// WorkloadIdentityTokenSource returns the token source CreatePublisher gives
// the Pub/Sub client for a workload_identity destination.
func (d *GCPPubSubDestination) WorkloadIdentityTokenSource(dest *models.Destination) (oauth2.TokenSource, error) {
	_, creds, err := d.resolveMetadata(context.Background(), dest)
	if err != nil {
		return nil, err
	}
	return d.workloadIdentityTokenSource(dest.TenantID, creds)
}
