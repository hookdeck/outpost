package mocksdk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/hookdeck/outpost/internal/destinationmockserver"
	"github.com/hookdeck/outpost/internal/models"
)

func New(baseURL string) destinationmockserver.EntityStore {
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		panic(err)
	}
	return &sdk{
		client: &httpClient{
			baseURL: parsedURL,
			client:  &http.Client{},
		},
	}
}

type sdk struct {
	client *httpClient
}

func (sdk *sdk) ListDestination(ctx context.Context) ([]models.Destination, error) {
	resp, err := sdk.client.get(ctx, "/destinations")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var destinations []models.Destination
	if err := json.NewDecoder(resp.Body).Decode(&destinations); err != nil {
		return nil, err
	}

	return destinations, nil
}

func (sdk *sdk) RetrieveDestination(ctx context.Context, id string) (*models.Destination, error) {
	resp, err := sdk.client.get(ctx, "/destinations/"+id)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var destination models.Destination
	if err := json.NewDecoder(resp.Body).Decode(&destination); err != nil {
		return nil, err
	}

	return &destination, nil
}

func (sdk *sdk) UpsertDestination(ctx context.Context, destination models.Destination) error {
	resp, err := sdk.client.put(ctx, "/destinations", destination)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to upsert destination, status code: %d", resp.StatusCode)
	}

	return nil
}

func (sdk *sdk) DeleteDestination(ctx context.Context, id string) error {
	resp, err := sdk.client.delete(ctx, "/destinations/"+id)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to delete destination, status code: %d", resp.StatusCode)
	}

	return nil
}

func (sdk *sdk) ReceiveEvent(ctx context.Context, destinationID string, payload map[string]interface{}, metadata map[string]string) (*destinationmockserver.Event, error) {
	// TODO: send metadata as headers
	resp, err := sdk.client.post(ctx, "/webhook/"+destinationID, payload)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to receive event, status code: %d", resp.StatusCode)
	}
	var event destinationmockserver.Event
	if err := json.NewDecoder(resp.Body).Decode(&event); err != nil {
		return nil, err
	}

	return &event, nil
}

func (sdk *sdk) ListEvent(ctx context.Context, destinationID string) ([]destinationmockserver.Event, error) {
	resp, err := sdk.client.get(ctx, "/destinations/"+destinationID+"/events")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var events []destinationmockserver.Event
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		return nil, err
	}

	return events, nil
}
