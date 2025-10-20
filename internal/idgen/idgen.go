package idgen

import (
	"fmt"

	"github.com/google/uuid"
	gonanoid "github.com/matoous/go-nanoid/v2"
)

var (
	globalGenerator *IDGenerator
)

func init() {
	// Initialize with default UUID v4 generator
	globalGenerator = &IDGenerator{
		generator:         &uuidv4Generator{},
		eventPrefix:       "",
		destinationPrefix: "",
		deliveryPrefix:    "",
	}
}

type idGenerator interface {
	generate() string
}

type IDGenerator struct {
	generator         idGenerator
	eventPrefix       string
	destinationPrefix string
	deliveryPrefix    string
}

func (g *IDGenerator) Event() string {
	return g.generate(g.eventPrefix)
}

func (g *IDGenerator) Destination() string {
	return g.generate(g.destinationPrefix)
}

func (g *IDGenerator) Delivery() string {
	return g.generate(g.deliveryPrefix)
}

func (g *IDGenerator) generate(prefix string) string {
	id := g.generator.generate()
	if prefix != "" {
		return prefix + "_" + id
	}
	return id
}

// newIDGenerator creates a new ID generator with the given type
func newIDGenerator(idType string) (idGenerator, error) {
	if idType == "" {
		idType = "uuidv4"
	}

	// Select the appropriate generator implementation
	switch idType {
	case "uuidv4":
		return &uuidv4Generator{}, nil
	case "uuidv7":
		return &uuidv7Generator{}, nil
	case "nanoid":
		return &nanoidGenerator{}, nil
	default:
		return nil, fmt.Errorf("invalid id type: %s (must be one of: uuidv4, uuidv7, nanoid)", idType)
	}
}

// Generator implementations

type uuidv4Generator struct{}

func (g *uuidv4Generator) generate() string {
	return uuid.New().String()
}

type uuidv7Generator struct{}

func (g *uuidv7Generator) generate() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New().String()
	}
	return id.String()
}

type nanoidGenerator struct{}

func (g *nanoidGenerator) generate() string {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	const length = 26

	id, err := gonanoid.Generate(alphabet, length)
	if err != nil {
		return uuid.New().String()
	}
	return id
}

type IDGenConfig struct {
	Type              string
	EventPrefix       string
	DestinationPrefix string
	DeliveryPrefix    string
}

func Configure(cfg IDGenConfig) error {
	gen, err := newIDGenerator(cfg.Type)
	if err != nil {
		return fmt.Errorf("failed to configure ID generator: %w", err)
	}

	globalGenerator = &IDGenerator{
		generator:         gen,
		eventPrefix:       cfg.EventPrefix,
		destinationPrefix: cfg.DestinationPrefix,
		deliveryPrefix:    cfg.DeliveryPrefix,
	}

	return nil
}

func Event() string {
	return globalGenerator.Event()
}

func Destination() string {
	return globalGenerator.Destination()
}

func Delivery() string {
	return globalGenerator.Delivery()
}
