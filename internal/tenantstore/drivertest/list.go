package drivertest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/pagination/paginationtest"
	"github.com/hookdeck/outpost/internal/tenantstore/driver"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// multiDestinationData holds shared test data for multi-destination tests.
type multiDestinationData struct {
	tenant       models.Tenant
	destinations []models.Destination
}

func setupMultiDestination(t *testing.T, ctx context.Context, store driver.TenantStore) multiDestinationData {
	t.Helper()
	data := multiDestinationData{
		tenant: models.Tenant{
			ID:        idgen.String(),
			CreatedAt: time.Now(),
		},
		destinations: make([]models.Destination, 5),
	}
	require.NoError(t, store.UpsertTenant(ctx, data.tenant))

	destinationTopicList := [][]string{
		{"*"},
		{"user.created"},
		{"user.updated"},
		{"user.deleted"},
		{"user.created", "user.updated"},
	}
	baseTime := time.Now().Add(-10 * time.Second).Truncate(time.Second)
	for i := 0; i < 5; i++ {
		data.destinations[i] = testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID(idgen.Destination()),
			testutil.DestinationFactory.WithTenantID(data.tenant.ID),
			testutil.DestinationFactory.WithTopics(destinationTopicList[i]),
			testutil.DestinationFactory.WithCreatedAt(baseTime.Add(time.Duration(i)*time.Second)),
		)
		require.NoError(t, store.UpsertDestination(ctx, data.destinations[i]))
	}

	// Insert & Delete destination to ensure cleanup
	toBeDeletedID := idgen.Destination()
	require.NoError(t, store.UpsertDestination(ctx,
		testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithID(toBeDeletedID),
			testutil.DestinationFactory.WithTenantID(data.tenant.ID),
			testutil.DestinationFactory.WithTopics([]string{"*"}),
		)))
	require.NoError(t, store.DeleteDestination(ctx, data.tenant.ID, toBeDeletedID))

	return data
}

func testList(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	t.Run("MultiDestinationRetrieveTenantDestinationsCount", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)
		data := setupMultiDestination(t, ctx, store)

		tenant, err := store.RetrieveTenant(ctx, data.tenant.ID)
		require.NoError(t, err)
		require.Equal(t, 5, tenant.DestinationsCount)
	})

	t.Run("MultiDestinationRetrieveTenantTopics", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)
		data := setupMultiDestination(t, ctx, store)

		// destinations[0] has topics ["*"]
		tenant, err := store.RetrieveTenant(ctx, data.tenant.ID)
		require.NoError(t, err)
		require.Equal(t, []string{"*"}, tenant.Topics)

		// After deleting wildcard destination, topics should aggregate remaining
		require.NoError(t, store.DeleteDestination(ctx, data.tenant.ID, data.destinations[0].ID))
		tenant, err = store.RetrieveTenant(ctx, data.tenant.ID)
		require.NoError(t, err)
		require.Equal(t, []string{"user.created", "user.deleted", "user.updated"}, tenant.Topics)

		require.NoError(t, store.DeleteDestination(ctx, data.tenant.ID, data.destinations[1].ID))
		tenant, err = store.RetrieveTenant(ctx, data.tenant.ID)
		require.NoError(t, err)
		require.Equal(t, []string{"user.created", "user.deleted", "user.updated"}, tenant.Topics)

		require.NoError(t, store.DeleteDestination(ctx, data.tenant.ID, data.destinations[2].ID))
		tenant, err = store.RetrieveTenant(ctx, data.tenant.ID)
		require.NoError(t, err)
		require.Equal(t, []string{"user.created", "user.deleted", "user.updated"}, tenant.Topics)

		require.NoError(t, store.DeleteDestination(ctx, data.tenant.ID, data.destinations[3].ID))
		tenant, err = store.RetrieveTenant(ctx, data.tenant.ID)
		require.NoError(t, err)
		require.Equal(t, []string{"user.created", "user.updated"}, tenant.Topics)

		require.NoError(t, store.DeleteDestination(ctx, data.tenant.ID, data.destinations[4].ID))
		tenant, err = store.RetrieveTenant(ctx, data.tenant.ID)
		require.NoError(t, err)
		require.Equal(t, []string{}, tenant.Topics)
	})

	t.Run("MultiDestinationListDestinationByTenant", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)
		data := setupMultiDestination(t, ctx, store)

		destinations, err := store.ListDestinationByTenant(ctx, data.tenant.ID)
		require.NoError(t, err)
		require.Len(t, destinations, 5)
		for index, destination := range destinations {
			require.Equal(t, data.destinations[index].ID, destination.ID)
		}
	})

	t.Run("MultiDestinationListDestinationWithOpts", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)
		data := setupMultiDestination(t, ctx, store)

		t.Run("filter by type: webhook", func(t *testing.T) {
			destinations, err := store.ListDestinationByTenant(ctx, data.tenant.ID, driver.WithDestinationFilter(driver.DestinationFilter{
				Type: []string{"webhook"},
			}))
			require.NoError(t, err)
			require.Len(t, destinations, 5)
		})

		t.Run("filter by type: rabbitmq", func(t *testing.T) {
			destinations, err := store.ListDestinationByTenant(ctx, data.tenant.ID, driver.WithDestinationFilter(driver.DestinationFilter{
				Type: []string{"rabbitmq"},
			}))
			require.NoError(t, err)
			require.Len(t, destinations, 0)
		})

		t.Run("filter by type: webhook,rabbitmq", func(t *testing.T) {
			destinations, err := store.ListDestinationByTenant(ctx, data.tenant.ID, driver.WithDestinationFilter(driver.DestinationFilter{
				Type: []string{"webhook", "rabbitmq"},
			}))
			require.NoError(t, err)
			require.Len(t, destinations, 5)
		})

		t.Run("filter by topic: user.created", func(t *testing.T) {
			destinations, err := store.ListDestinationByTenant(ctx, data.tenant.ID, driver.WithDestinationFilter(driver.DestinationFilter{
				Topics: []string{"user.created"},
			}))
			require.NoError(t, err)
			require.Len(t, destinations, 3)
		})

		t.Run("filter by topic: user.created,user.updated", func(t *testing.T) {
			destinations, err := store.ListDestinationByTenant(ctx, data.tenant.ID, driver.WithDestinationFilter(driver.DestinationFilter{
				Topics: []string{"user.created", "user.updated"},
			}))
			require.NoError(t, err)
			require.Len(t, destinations, 2)
		})

		t.Run("filter by type: rabbitmq, topic: user.created,user.updated", func(t *testing.T) {
			destinations, err := store.ListDestinationByTenant(ctx, data.tenant.ID, driver.WithDestinationFilter(driver.DestinationFilter{
				Type:   []string{"rabbitmq"},
				Topics: []string{"user.created", "user.updated"},
			}))
			require.NoError(t, err)
			require.Len(t, destinations, 0)
		})

		t.Run("filter by topic: *", func(t *testing.T) {
			destinations, err := store.ListDestinationByTenant(ctx, data.tenant.ID, driver.WithDestinationFilter(driver.DestinationFilter{
				Topics: []string{"*"},
			}))
			require.NoError(t, err)
			require.Len(t, destinations, 1)
		})
	})

}

func testListTenant(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	t.Run("Enrichment", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)
		require.NoError(t, store.Init(ctx))

		// Create 25 tenants
		tenants := make([]models.Tenant, 25)
		baseTime := time.Now()
		for i := range tenants {
			tenants[i] = testutil.TenantFactory.Any(
				testutil.TenantFactory.WithCreatedAt(baseTime.Add(time.Duration(i)*time.Second)),
				testutil.TenantFactory.WithUpdatedAt(baseTime.Add(time.Duration(i)*time.Second)),
			)
			require.NoError(t, store.UpsertTenant(ctx, tenants[i]))
		}
		tenantWithDests := tenants[24]
		for i := range 2 {
			dest := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithID(fmt.Sprintf("dest_suite_%d", i)),
				testutil.DestinationFactory.WithTenantID(tenantWithDests.ID),
			)
			require.NoError(t, store.UpsertDestination(ctx, dest))
		}

		t.Run("returns total count independent of pagination", func(t *testing.T) {
			resp1, err := store.ListTenant(ctx, driver.ListTenantRequest{Limit: 2})
			require.NoError(t, err)
			assert.Equal(t, 25, resp1.Count)
			assert.Len(t, resp1.Models, 2)

			var nextCursor string
			if resp1.Pagination.Next != nil {
				nextCursor = *resp1.Pagination.Next
			}
			resp2, err := store.ListTenant(ctx, driver.ListTenantRequest{Limit: 2, Next: nextCursor})
			require.NoError(t, err)
			assert.Equal(t, 25, resp2.Count)
		})

		t.Run("does not include destinations in tenant list", func(t *testing.T) {
			resp, err := store.ListTenant(ctx, driver.ListTenantRequest{Limit: 100})
			require.NoError(t, err)
			assert.Equal(t, 25, resp.Count)
			for _, tenant := range resp.Models {
				assert.NotContains(t, tenant.ID, "dest_")
			}
		})

		t.Run("returns destinations_count and topics", func(t *testing.T) {
			resp, err := store.ListTenant(ctx, driver.ListTenantRequest{Limit: 100})
			require.NoError(t, err)

			var found *models.Tenant
			for i := range resp.Models {
				if resp.Models[i].ID == tenantWithDests.ID {
					found = &resp.Models[i]
					break
				}
			}
			require.NotNil(t, found)
			assert.Equal(t, 2, found.DestinationsCount)
			assert.NotNil(t, found.Topics)

			var without *models.Tenant
			for i := range resp.Models {
				if resp.Models[i].ID != tenantWithDests.ID {
					without = &resp.Models[i]
					break
				}
			}
			require.NotNil(t, without)
			assert.Equal(t, 0, without.DestinationsCount)
			assert.Empty(t, without.Topics)
		})
	})

	t.Run("ExcludesDeleted", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)
		require.NoError(t, store.Init(ctx))

		// Create initial tenants
		for i := 0; i < 5; i++ {
			require.NoError(t, store.UpsertTenant(ctx, testutil.TenantFactory.Any()))
		}

		t.Run("deleted tenant not returned", func(t *testing.T) {
			initialResp, err := store.ListTenant(ctx, driver.ListTenantRequest{})
			require.NoError(t, err)
			initialCount := initialResp.Count

			tenant1 := testutil.TenantFactory.Any()
			tenant2 := testutil.TenantFactory.Any()
			require.NoError(t, store.UpsertTenant(ctx, tenant1))
			require.NoError(t, store.UpsertTenant(ctx, tenant2))

			resp, err := store.ListTenant(ctx, driver.ListTenantRequest{})
			require.NoError(t, err)
			assert.Equal(t, initialCount+2, resp.Count)

			require.NoError(t, store.DeleteTenant(ctx, tenant1.ID))

			resp, err = store.ListTenant(ctx, driver.ListTenantRequest{})
			require.NoError(t, err)
			assert.Equal(t, initialCount+1, resp.Count)

			for _, tenant := range resp.Models {
				assert.NotEqual(t, tenant1.ID, tenant.ID)
			}

			_ = store.DeleteTenant(ctx, tenant2.ID)
		})
	})

	t.Run("InputValidation", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)
		require.NoError(t, store.Init(ctx))

		// Create 25 tenants for pagination tests
		tenants := make([]models.Tenant, 25)
		baseTime := time.Now()
		for i := range tenants {
			tenants[i] = testutil.TenantFactory.Any(
				testutil.TenantFactory.WithCreatedAt(baseTime.Add(time.Duration(i)*time.Second)),
				testutil.TenantFactory.WithUpdatedAt(baseTime.Add(time.Duration(i)*time.Second)),
			)
			require.NoError(t, store.UpsertTenant(ctx, tenants[i]))
		}

		t.Run("invalid dir returns error", func(t *testing.T) {
			_, err := store.ListTenant(ctx, driver.ListTenantRequest{Dir: "invalid"})
			require.Error(t, err)
			assert.ErrorIs(t, err, driver.ErrInvalidOrder)
		})

		t.Run("conflicting cursors returns error", func(t *testing.T) {
			_, err := store.ListTenant(ctx, driver.ListTenantRequest{
				Next: "somecursor",
				Prev: "anothercursor",
			})
			require.Error(t, err)
			assert.ErrorIs(t, err, driver.ErrConflictingCursors)
		})

		t.Run("invalid next cursor returns error", func(t *testing.T) {
			_, err := store.ListTenant(ctx, driver.ListTenantRequest{
				Next: "not-valid-base62!!!",
			})
			require.Error(t, err)
			assert.ErrorIs(t, err, driver.ErrInvalidCursor)
		})

		t.Run("invalid prev cursor returns error", func(t *testing.T) {
			_, err := store.ListTenant(ctx, driver.ListTenantRequest{
				Prev: "not-valid-base62!!!",
			})
			require.Error(t, err)
			assert.ErrorIs(t, err, driver.ErrInvalidCursor)
		})

		t.Run("malformed cursor format returns error", func(t *testing.T) {
			_, err := store.ListTenant(ctx, driver.ListTenantRequest{
				Next: "abc123",
			})
			require.Error(t, err)
			assert.ErrorIs(t, err, driver.ErrInvalidCursor)
		})

		t.Run("limit zero uses default", func(t *testing.T) {
			resp, err := store.ListTenant(ctx, driver.ListTenantRequest{Limit: 0})
			require.NoError(t, err)
			assert.Equal(t, 20, len(resp.Models))
			assert.Equal(t, 25, resp.Count)
		})

		t.Run("limit negative uses default", func(t *testing.T) {
			resp, err := store.ListTenant(ctx, driver.ListTenantRequest{Limit: -5})
			require.NoError(t, err)
			assert.NotNil(t, resp)
		})

		t.Run("limit exceeding max is capped", func(t *testing.T) {
			resp, err := store.ListTenant(ctx, driver.ListTenantRequest{Limit: 1000})
			require.NoError(t, err)
			assert.NotNil(t, resp)
			assert.Equal(t, 25, len(resp.Models))
			assert.Equal(t, 25, resp.Count)
		})

		t.Run("empty dir uses default desc", func(t *testing.T) {
			resp, err := store.ListTenant(ctx, driver.ListTenantRequest{Dir: ""})
			require.NoError(t, err)
			require.Len(t, resp.Models, 20)
			assert.Equal(t, tenants[24].ID, resp.Models[0].ID)
			assert.Equal(t, tenants[23].ID, resp.Models[1].ID)
		})
	})

	t.Run("IDFilter", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)
		require.NoError(t, store.Init(ctx))

		id1 := `tenant-'alpha'-(beta)-[gamma]-{delta}|omega`
		id2 := `tenant\path-'prod'-(v2)-[blue]-{green}|canary`

		require.NoError(t, store.UpsertTenant(ctx, models.Tenant{ID: id1, CreatedAt: time.Now(), UpdatedAt: time.Now()}))
		require.NoError(t, store.UpsertTenant(ctx, models.Tenant{ID: id2, CreatedAt: time.Now().Add(time.Second), UpdatedAt: time.Now().Add(time.Second)}))
		require.NoError(t, store.UpsertTenant(ctx, models.Tenant{ID: "plain-tenant", CreatedAt: time.Now().Add(2 * time.Second), UpdatedAt: time.Now().Add(2 * time.Second)}))

		resp, err := store.ListTenant(ctx, driver.ListTenantRequest{
			Limit: 100,
			ID:    []string{id1, id2},
		})
		require.NoError(t, err)
		require.Len(t, resp.Models, 2)
		assert.Equal(t, 2, resp.Count)
		assert.ElementsMatch(t, []string{id1, id2}, []string{
			resp.Models[0].ID,
			resp.Models[1].ID,
		})
	})

	t.Run("KeysetPagination", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)
		require.NoError(t, store.Init(ctx))

		t.Run("add during traversal does not cause duplicate", func(t *testing.T) {
			prefix := fmt.Sprintf("add_edge_%d_", time.Now().UnixNano())
			tenantIDs := make([]string, 15)
			baseTime := time.Now().Add(20 * time.Hour)
			for i := 0; i < 15; i++ {
				tenantIDs[i] = fmt.Sprintf("%s%02d", prefix, i)
				tenant := models.Tenant{
					ID:        tenantIDs[i],
					CreatedAt: baseTime.Add(time.Duration(i) * time.Second),
					UpdatedAt: baseTime,
				}
				require.NoError(t, store.UpsertTenant(ctx, tenant))
			}

			resp1, err := store.ListTenant(ctx, driver.ListTenantRequest{Limit: 5})
			require.NoError(t, err)
			require.Len(t, resp1.Models, 5)

			// Add new tenant with newest timestamp
			newTenantID := prefix + "NEW"
			newTenant := models.Tenant{
				ID:        newTenantID,
				CreatedAt: baseTime.Add(time.Hour),
				UpdatedAt: baseTime,
			}
			require.NoError(t, store.UpsertTenant(ctx, newTenant))
			tenantIDs = append(tenantIDs, newTenantID)

			var nextCursor string
			if resp1.Pagination.Next != nil {
				nextCursor = *resp1.Pagination.Next
			}
			resp2, err := store.ListTenant(ctx, driver.ListTenantRequest{
				Limit: 5,
				Next:  nextCursor,
			})
			require.NoError(t, err)
			require.NotEmpty(t, resp2.Models)

			page1IDs := make(map[string]bool)
			for _, tenant := range resp1.Models {
				page1IDs[tenant.ID] = true
			}
			for _, tenant := range resp2.Models {
				assert.False(t, page1IDs[tenant.ID],
					"keyset pagination: no duplicates, but found %s", tenant.ID)
			}

			// Cleanup
			for _, id := range tenantIDs {
				_ = store.DeleteTenant(ctx, id)
			}
		})
	})

	t.Run("PaginationSuite", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)
		require.NoError(t, store.Init(ctx))

		var createdTenantIDs []string
		baseTime := time.Now()

		suite := paginationtest.Suite[models.Tenant]{
			Name: "ListTenant",

			NewItem: func(index int) models.Tenant {
				return models.Tenant{
					ID:        fmt.Sprintf("tenant_pagination_%d_%d", time.Now().UnixNano(), index),
					CreatedAt: baseTime.Add(time.Duration(index) * time.Second),
					UpdatedAt: baseTime.Add(time.Duration(index) * time.Second),
				}
			},

			InsertMany: func(ctx context.Context, items []models.Tenant) error {
				for _, item := range items {
					if err := store.UpsertTenant(ctx, item); err != nil {
						return err
					}
					createdTenantIDs = append(createdTenantIDs, item.ID)
				}
				return nil
			},

			List: func(ctx context.Context, opts paginationtest.ListOpts) (paginationtest.ListResult[models.Tenant], error) {
				resp, err := store.ListTenant(ctx, driver.ListTenantRequest{
					Limit: opts.Limit,
					Dir:   opts.Order,
					Next:  opts.Next,
					Prev:  opts.Prev,
				})
				if err != nil {
					return paginationtest.ListResult[models.Tenant]{}, err
				}
				var next, prev string
				if resp.Pagination.Next != nil {
					next = *resp.Pagination.Next
				}
				if resp.Pagination.Prev != nil {
					prev = *resp.Pagination.Prev
				}
				return paginationtest.ListResult[models.Tenant]{
					Items: resp.Models,
					Next:  next,
					Prev:  prev,
				}, nil
			},

			GetID: func(t models.Tenant) string {
				return t.ID
			},

			Cleanup: func(ctx context.Context) error {
				for _, id := range createdTenantIDs {
					_ = store.DeleteTenant(ctx, id)
				}
				createdTenantIDs = nil
				return nil
			},
		}

		suite.Run(t)
	})

	t.Run("IDFilterSpecialChars", func(t *testing.T) {
		ctx := context.Background()
		h, err := newHarness(ctx, t)
		require.NoError(t, err)
		t.Cleanup(h.Close)

		store, err := h.MakeDriver(ctx)
		require.NoError(t, err)
		require.NoError(t, store.Init(ctx))

		// Each entry is a test name + tenant ID with characters that could break
		// RediSearch TAG queries if not properly escaped.
		weirdIDs := []struct {
			name string
			id   string
		}{
			// Separator characters
			{"dots", "tnt.dots.here"},
			{"dashes", "tnt-dashes-here"},
			{"colons", "tnt:colons:here"},
			// TODO: edge case — comma is the default RediSearch TAG separator, so "tnt,comma" gets
			// indexed as two tags ("tnt" and "comma") instead of one. Fixing requires changing the
			// index SEPARATOR to "\x00" via a Redis migration (FT.DROPINDEX + recreate).
			// {"comma", "tnt,comma"},
			{"semicolon", "tnt;semi"},
			{"slash", "tnt/slash"},

			// Query syntax characters
			{"at_sign", "tnt@at"},
			{"star", "tnt*star"},
			{"bang", "tnt!bang"},
			{"tilde", "tnt~tilde"},
			{"hash", "tnt#hash"},
			{"dollar", "tnt$dollar"},
			{"percent", "tnt%pct"},
			{"caret", "tnt^caret"},
			{"ampersand", "tnt&amp"},
			{"plus", "tnt+plus"},
			{"equals", "tnt=equals"},

			// TAG structural characters
			{"open_brace", "tnt{brace"},
			{"close_brace", "tnt}brace"},
			{"pipe", "tnt|pipe"},
			{"backslash", `tnt\backslash`},
			{"open_bracket", "tnt[bracket"},
			{"close_bracket", "tnt]bracket"},
			{"open_paren", "tnt(paren"},
			{"close_paren", "tnt)paren"},

			// Quoting characters
			{"double_quote", `tnt"quote`},
			{"single_quote", "tnt'quote"},
			{"backtick", "tnt`tick"},

			// Whitespace
			{"space", "tnt with spaces"},

			// Angle brackets
			{"angle_brackets", "tnt<>angle"},
			{"question_mark", "tnt?question"},

			// Injection-style IDs that mimic RediSearch query syntax
			{"injection_tag_breakout", `tnt" @entity:{hack}`},
			{"injection_negation", "-@deleted_at:[1 +inf]"},
			{"injection_field_ref", "@id:{evil}"},

			// Combinatorial stress: many special chars together
			{"kitchen_sink", `a]b[c{d}e"f\g|h.i-j@k`},
		}

		// Create all tenants plus a "normal" control tenant.
		controlID := fmt.Sprintf("tnt_control_%d", time.Now().UnixNano())
		baseTime := time.Now().Add(30 * time.Hour) // offset to avoid collision with other subtests
		var allIDs []string

		require.NoError(t, store.UpsertTenant(ctx, models.Tenant{
			ID:        controlID,
			CreatedAt: baseTime,
			UpdatedAt: baseTime,
		}))
		allIDs = append(allIDs, controlID)

		for i, tc := range weirdIDs {
			tenant := models.Tenant{
				ID:        tc.id,
				CreatedAt: baseTime.Add(time.Duration(i+1) * time.Second),
				UpdatedAt: baseTime.Add(time.Duration(i+1) * time.Second),
			}
			require.NoError(t, store.UpsertTenant(ctx, tenant), "failed to upsert tenant %q", tc.name)
			allIDs = append(allIDs, tc.id)
		}

		t.Cleanup(func() {
			for _, id := range allIDs {
				_ = store.DeleteTenant(ctx, id)
			}
		})

		t.Run("single ID filter", func(t *testing.T) {
			for _, tc := range weirdIDs {
				t.Run(tc.name, func(t *testing.T) {
					resp, err := store.ListTenant(ctx, driver.ListTenantRequest{
						Limit: 100,
						ID:    []string{tc.id},
					})
					require.NoError(t, err, "ListTenant with ID %q should not error", tc.id)
					require.Len(t, resp.Models, 1, "expected exactly 1 tenant for ID %q", tc.id)
					assert.Equal(t, tc.id, resp.Models[0].ID)
				})
			}
		})

		t.Run("multi ID filter with mixed special chars", func(t *testing.T) {
			ids := []string{
				"tnt.dots.here",
				"tnt-dashes-here",
				`tnt"quote`,
				`tnt\backslash`,
				"tnt|pipe",
				controlID,
			}
			resp, err := store.ListTenant(ctx, driver.ListTenantRequest{
				Limit: 100,
				ID:    ids,
			})
			require.NoError(t, err)
			assert.Len(t, resp.Models, len(ids))

			gotIDs := make(map[string]bool)
			for _, m := range resp.Models {
				gotIDs[m.ID] = true
			}
			for _, id := range ids {
				assert.True(t, gotIDs[id], "expected ID %q in results", id)
			}
		})

		t.Run("ID filter does not match unrelated tenants", func(t *testing.T) {
			resp, err := store.ListTenant(ctx, driver.ListTenantRequest{
				Limit: 100,
				ID:    []string{"tnt.dots.here"},
			})
			require.NoError(t, err)
			require.Len(t, resp.Models, 1)
			assert.Equal(t, "tnt.dots.here", resp.Models[0].ID)
			// Ensure the control tenant is NOT returned
			for _, m := range resp.Models {
				assert.NotEqual(t, controlID, m.ID)
			}
		})

		t.Run("ID filter with nonexistent weird ID returns empty", func(t *testing.T) {
			resp, err := store.ListTenant(ctx, driver.ListTenantRequest{
				Limit: 100,
				ID:    []string{`nonexistent"weird|id`},
			})
			require.NoError(t, err)
			assert.Empty(t, resp.Models)
		})

		t.Run("count reflects filtered set", func(t *testing.T) {
			ids := []string{"tnt.dots.here", "tnt-dashes-here"}
			resp, err := store.ListTenant(ctx, driver.ListTenantRequest{
				Limit: 100,
				ID:    ids,
			})
			require.NoError(t, err)
			assert.Equal(t, 2, resp.Count)
		})
	})
}
