package destawssqs_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destawssqs"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AWS spells the message key of an error body "message" or "Message"
// depending on the error; both must reach the delivery response.
// AWS_MAX_ATTEMPTS=1 skips the SDK's retries; t.Setenv rules out t.Parallel.
func TestAWSSQSPublisher_APIErrorMessage(t *testing.T) {
	for _, typ := range []string{"com.amazonaws.sqs#QueueDoesNotExist", "com.amazonaws.sqs#InvalidAddress"} {
		for _, key := range []string{"message", "Message"} {
			t.Run(typ+"/"+key, func(t *testing.T) {
				msg := "User: arn:aws:iam::123456789012:user/outpost is not authorized (" + typ + ")"
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/x-amz-json-1.0")
					w.WriteHeader(400)
					_, _ = w.Write([]byte(`{"__type":"` + typ + `","` + key + `":"` + msg + `"}`))
				}))
				defer srv.Close()
				t.Setenv("AWS_MAX_ATTEMPTS", "1")
				p, err := destawssqs.New(testutil.Registry.MetadataLoader(), nil)
				require.NoError(t, err)
				d := testutil.DestinationFactory.Any(
					testutil.DestinationFactory.WithType("aws_sqs"),
					testutil.DestinationFactory.WithConfig(map[string]string{"endpoint": srv.URL, "queue_url": srv.URL + "/000000000000/q"}),
					testutil.DestinationFactory.WithCredentials(map[string]string{"key": "test", "secret": "test"}),
				)
				pub, err := p.CreatePublisher(context.Background(), &d)
				require.NoError(t, err)
				defer pub.Close()
				ev := testutil.EventFactory.Any()
				del, err := pub.Publish(context.Background(), &ev)
				require.Error(t, err)
				require.NotNil(t, del)
				assert.Contains(t, del.Response["error"], msg)
			})
		}
	}
}
