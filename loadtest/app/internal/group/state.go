package group

import "fmt"

type State string

const (
	StateCreated      State = "created"
	StateProvisioning State = "provisioning"
	StateProvisioned  State = "provisioned"
	StateRunning      State = "running"
	StateStopping     State = "stopping"
	StateStopped      State = "stopped"
	StateError        State = "error"
)

var validTransitions = map[State][]State{
	StateCreated:      {StateProvisioning},
	StateProvisioning: {StateProvisioned, StateError},
	StateProvisioned:  {StateRunning, StateCreated},
	StateRunning:      {StateStopping},
	StateStopping:     {StateStopped, StateError},
	StateStopped:      {StateRunning, StateProvisioned, StateCreated},
	StateError:        {StateProvisioning, StateCreated},
}

func ValidateTransition(from, to State) error {
	allowed, ok := validTransitions[from]
	if !ok {
		return fmt.Errorf("unknown state: %s", from)
	}
	for _, s := range allowed {
		if s == to {
			return nil
		}
	}
	return fmt.Errorf("invalid transition: %s → %s", from, to)
}
