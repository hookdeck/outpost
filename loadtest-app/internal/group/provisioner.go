package group

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/hookdeck/outpost/loadtest-app/internal/mock"
	"github.com/hookdeck/outpost/loadtest-app/internal/outpost"
)

type Provisioner struct {
	client     *outpost.Client
	mockServer *mock.Server
	mockURL    string
}

func NewProvisioner(client *outpost.Client, mockServer *mock.Server, mockURL string) *Provisioner {
	return &Provisioner{
		client:     client,
		mockServer: mockServer,
		mockURL:    mockURL,
	}
}

func (p *Provisioner) Provision(ctx context.Context, g *Group) error {
	if err := g.Transition(StateProvisioning); err != nil {
		return err
	}

	type tenantWork struct {
		index int
	}

	work := make(chan tenantWork, g.Config.TenantCount)
	for i := 0; i < g.Config.TenantCount; i++ {
		work <- tenantWork{index: i}
	}
	close(work)

	var mu sync.Mutex
	var firstErr error
	provisioned := make([]ProvisionedTenant, g.Config.TenantCount)

	workers := 10
	if g.Config.TenantCount < workers {
		workers = g.Config.TenantCount
	}

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for tw := range work {
				if ctx.Err() != nil {
					return
				}

				tenantID := g.TenantID(tw.index)
				pt, err := p.provisionTenant(ctx, g, tenantID, tw.index)
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					slog.Error("provision tenant failed", "tenant", tenantID, "error", err)
					return
				}

				mu.Lock()
				provisioned[tw.index] = pt
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if firstErr != nil {
		g.SetError(firstErr)
		return firstErr
	}

	g.ProvisionedTenants = provisioned
	return g.Transition(StateProvisioned)
}

func (p *Provisioner) provisionTenant(ctx context.Context, g *Group, tenantID string, tenantIndex int) (ProvisionedTenant, error) {
	if err := p.client.CreateTenant(ctx, tenantID); err != nil {
		return ProvisionedTenant{}, fmt.Errorf("create tenant %s: %w", tenantID, err)
	}

	pt := ProvisionedTenant{TenantID: tenantID}

	for d := 0; d < g.Config.DestinationsPerTenant; d++ {
		destIdx := fmt.Sprintf("%d", d)
		tenantIdx := fmt.Sprintf("%d", tenantIndex)
		webhookURL := fmt.Sprintf("%s/webhook/%s/%s/%s", p.mockURL, g.Config.Name, tenantIdx, destIdx)

		destID, err := p.client.CreateDestination(ctx, tenantID, g.Config.Topics, webhookURL)
		if err != nil {
			return ProvisionedTenant{}, fmt.Errorf("create destination %d for %s: %w", d, tenantID, err)
		}
		pt.DestinationIDs = append(pt.DestinationIDs, destID)

		// Register mock route
		p.mockServer.RegisterRoute(g.Config.Name, tenantIdx, destIdx, g.MockProfile)
	}

	slog.Debug("provisioned tenant", "tenant", tenantID, "destinations", len(pt.DestinationIDs))
	return pt, nil
}

func (p *Provisioner) Teardown(ctx context.Context, g *Group) error {
	for i, pt := range g.ProvisionedTenants {
		if pt.TenantID == "" {
			continue
		}
		if err := p.client.DeleteTenant(ctx, pt.TenantID); err != nil {
			slog.Warn("teardown: delete tenant failed", "tenant", pt.TenantID, "error", err)
		}

		// Unregister mock routes
		tenantIdx := fmt.Sprintf("%d", i)
		for d := 0; d < g.Config.DestinationsPerTenant; d++ {
			destIdx := fmt.Sprintf("%d", d)
			p.mockServer.UnregisterRoute(g.Config.Name, tenantIdx, destIdx)
		}
	}

	g.ProvisionedTenants = nil
	return g.Transition(StateCreated)
}
