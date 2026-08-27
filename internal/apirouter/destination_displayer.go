package apirouter

import (
	"strings"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/models"
)

type destinationDisplayer struct {
	registry             destregistry.Registry
	topicsAllowWildcards bool
}

func newDestinationDisplayer(r destregistry.Registry, topicsAllowWildcards bool) *destinationDisplayer {
	return &destinationDisplayer{
		registry:             r,
		topicsAllowWildcards: topicsAllowWildcards,
	}
}

func (d *destinationDisplayer) Display(dest *models.Destination) (*destregistry.DestinationDisplay, error) {
	display, err := d.registry.DisplayDestination(dest)
	if err != nil {
		return nil, err
	}
	if !d.topicsAllowWildcards {
		displayDestination := *display.Destination
		displayDestination.Topics = filterWildcardTopicPatterns(display.Destination.Topics)
		display.Destination = &displayDestination
	}

	return display, nil
}

func (d *destinationDisplayer) DisplayList(destinations []models.Destination) ([]*destregistry.DestinationDisplay, error) {
	result := make([]*destregistry.DestinationDisplay, len(destinations))
	for i := range destinations {
		display, err := d.Display(&destinations[i])
		if err != nil {
			return nil, err
		}
		result[i] = display
	}
	return result, nil
}

func filterWildcardTopicPatterns(topics []string) []string {
	if topics == nil {
		return nil
	}

	filteredTopics := make([]string, 0, len(topics))
	for _, topic := range topics {
		if topic == "*" || !strings.Contains(topic, "*") {
			filteredTopics = append(filteredTopics, topic)
		}
	}

	return filteredTopics
}
