package redistenantstore

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/tenantstore/driver"
)

// destinationSummary is a package-private summary used for Redis storage.
type destinationSummary struct {
	ID       string        `json:"id"`
	Type     string        `json:"type"`
	Topics   models.Topics `json:"topics"`
	Filter   models.Filter `json:"filter,omitempty"`
	Disabled bool          `json:"disabled"`
}

func newDestinationSummary(d models.Destination) *destinationSummary {
	return &destinationSummary{
		ID:       d.ID,
		Type:     d.Type,
		Topics:   d.Topics,
		Filter:   d.Filter,
		Disabled: d.DisabledAt != nil,
	}
}

func (ds *destinationSummary) MarshalBinary() ([]byte, error) {
	return json.Marshal(ds)
}

func (ds *destinationSummary) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, ds)
}

// parseTenantHash parses a Redis hash map into a Tenant struct.
func parseTenantHash(hash map[string]string) (*models.Tenant, error) {
	if _, ok := hash["deleted_at"]; ok {
		return nil, driver.ErrTenantDeleted
	}
	if hash["id"] == "" {
		return nil, fmt.Errorf("missing id")
	}

	t := &models.Tenant{}
	t.ID = hash["id"]

	var err error
	t.CreatedAt, err = parseTimestamp(hash["created_at"])
	if err != nil {
		return nil, fmt.Errorf("invalid created_at: %w", err)
	}

	if hash["updated_at"] != "" {
		t.UpdatedAt, err = parseTimestamp(hash["updated_at"])
		if err != nil {
			t.UpdatedAt = t.CreatedAt
		}
	} else {
		t.UpdatedAt = t.CreatedAt
	}

	if metadataStr, exists := hash["metadata"]; exists && metadataStr != "" {
		err = t.Metadata.UnmarshalBinary([]byte(metadataStr))
		if err != nil {
			return nil, fmt.Errorf("invalid metadata: %w", err)
		}
	}

	return t, nil
}

// parseDestinationHash parses a Redis HGetAll command result into a Destination struct.
func parseDestinationHash(cmd *redis.MapStringStringCmd, tenantID string, cipher *aesCipher) (*models.Destination, error) {
	hash, err := cmd.Result()
	if err != nil {
		return nil, err
	}
	if len(hash) == 0 {
		return nil, redis.Nil
	}
	if _, exists := hash["deleted_at"]; exists {
		return nil, driver.ErrDestinationDeleted
	}

	d := &models.Destination{TenantID: tenantID}
	d.ID = hash["id"]
	d.Type = hash["type"]

	d.CreatedAt, err = parseTimestamp(hash["created_at"])
	if err != nil {
		return nil, fmt.Errorf("invalid created_at: %w", err)
	}

	if hash["updated_at"] != "" {
		d.UpdatedAt, err = parseTimestamp(hash["updated_at"])
		if err != nil {
			d.UpdatedAt = d.CreatedAt
		}
	} else {
		d.UpdatedAt = d.CreatedAt
	}

	if hash["disabled_at"] != "" {
		disabledAt, err := parseTimestamp(hash["disabled_at"])
		if err == nil {
			d.DisabledAt = &disabledAt
		}
	}

	if err := d.Topics.UnmarshalBinary([]byte(hash["topics"])); err != nil {
		return nil, fmt.Errorf("invalid topics: %w", err)
	}
	if err := d.Config.UnmarshalBinary([]byte(hash["config"])); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	credentialsBytes, err := cipher.decrypt([]byte(hash["credentials"]))
	if err != nil {
		return nil, fmt.Errorf("invalid credentials: %w", err)
	}
	if err := d.Credentials.UnmarshalBinary(credentialsBytes); err != nil {
		return nil, fmt.Errorf("invalid credentials: %w", err)
	}

	if deliveryMetadataStr, exists := hash["delivery_metadata"]; exists && deliveryMetadataStr != "" {
		deliveryMetadataBytes, err := cipher.decrypt([]byte(deliveryMetadataStr))
		if err != nil {
			return nil, fmt.Errorf("invalid delivery_metadata: %w", err)
		}
		if err := d.DeliveryMetadata.UnmarshalBinary(deliveryMetadataBytes); err != nil {
			return nil, fmt.Errorf("invalid delivery_metadata: %w", err)
		}
	}

	if metadataStr, exists := hash["metadata"]; exists && metadataStr != "" {
		if err := d.Metadata.UnmarshalBinary([]byte(metadataStr)); err != nil {
			return nil, fmt.Errorf("invalid metadata: %w", err)
		}
	}

	if filterStr, exists := hash["filter"]; exists && filterStr != "" {
		if err := d.Filter.UnmarshalBinary([]byte(filterStr)); err != nil {
			return nil, fmt.Errorf("invalid filter: %w", err)
		}
	}

	return d, nil
}

// parseTimestamp parses a timestamp from either numeric (Unix milliseconds) or RFC3339 format.
func parseTimestamp(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("missing timestamp")
	}

	if ts, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.UnixMilli(ts).UTC(), nil
	}

	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t, nil
	}

	return time.Parse(time.RFC3339, value)
}

// parseSearchResult parses an FT.SEARCH result (RESP2 or RESP3) into a list of tenants and total count.
func parseSearchResult(result interface{}) ([]models.Tenant, int, error) {
	if resultMap, ok := result.(map[interface{}]interface{}); ok {
		return parseResp3SearchResult(resultMap)
	}

	arr, ok := result.([]interface{})
	if !ok || len(arr) == 0 {
		return []models.Tenant{}, 0, nil
	}

	totalCount, ok := arr[0].(int64)
	if !ok {
		return nil, 0, fmt.Errorf("invalid search result: expected total count")
	}

	tenants := make([]models.Tenant, 0, (len(arr)-1)/2)

	for i := 1; i < len(arr); i += 2 {
		if i+1 >= len(arr) {
			break
		}

		hash := make(map[string]string)

		switch fields := arr[i+1].(type) {
		case []interface{}:
			for j := 0; j < len(fields)-1; j += 2 {
				key, keyOk := fields[j].(string)
				val, valOk := fields[j+1].(string)
				if keyOk && valOk {
					hash[key] = val
				}
			}
		case map[interface{}]interface{}:
			for k, v := range fields {
				key, keyOk := k.(string)
				if !keyOk {
					continue
				}
				switch val := v.(type) {
				case string:
					hash[key] = val
				case float64:
					hash[key] = fmt.Sprintf("%.0f", val)
				case int64:
					hash[key] = fmt.Sprintf("%d", val)
				}
			}
		default:
			continue
		}

		if _, deleted := hash["deleted_at"]; deleted {
			continue
		}

		tenant, err := parseTenantHash(hash)
		if err != nil {
			continue
		}

		tenants = append(tenants, *tenant)
	}

	return tenants, int(totalCount), nil
}

// parseResp3SearchResult parses a RESP3 FT.SEARCH result into a list of tenants and total count.
func parseResp3SearchResult(resultMap map[interface{}]interface{}) ([]models.Tenant, int, error) {
	totalCount := 0
	if tc, ok := resultMap["total_results"].(int64); ok {
		totalCount = int(tc)
	}

	results, ok := resultMap["results"].([]interface{})
	if !ok {
		return []models.Tenant{}, totalCount, nil
	}

	tenants := make([]models.Tenant, 0, len(results))

	for _, r := range results {
		docMap, ok := r.(map[interface{}]interface{})
		if !ok {
			continue
		}

		extraAttrs, ok := docMap["extra_attributes"].(map[interface{}]interface{})
		if !ok {
			continue
		}

		hash := make(map[string]string)
		for k, v := range extraAttrs {
			if keyStr, ok := k.(string); ok {
				if valStr, ok := v.(string); ok {
					hash[keyStr] = valStr
				}
			}
		}

		if _, deleted := hash["deleted_at"]; deleted {
			continue
		}

		tenant, err := parseTenantHash(hash)
		if err != nil {
			continue
		}

		tenants = append(tenants, *tenant)
	}

	return tenants, totalCount, nil
}

// parseListDestinationSummaryByTenantCmd parses a Redis HGetAll command result into destination summaries.
func parseListDestinationSummaryByTenantCmd(cmd *redis.MapStringStringCmd, opts driver.ListDestinationByTenantOpts) ([]destinationSummary, error) {
	destinationSummaryListHash, err := cmd.Result()
	if err != nil {
		if err == redis.Nil {
			return []destinationSummary{}, nil
		}
		return nil, err
	}
	destinationSummaryList := make([]destinationSummary, 0, len(destinationSummaryListHash))
	for _, destinationSummaryStr := range destinationSummaryListHash {
		ds := destinationSummary{}
		if err := ds.UnmarshalBinary([]byte(destinationSummaryStr)); err != nil {
			return nil, err
		}
		included := true
		if opts.Filter != nil {
			included = matchDestinationFilter(opts.Filter, ds)
		}
		if included {
			destinationSummaryList = append(destinationSummaryList, ds)
		}
	}
	return destinationSummaryList, nil
}

// parseTenantTopics extracts and deduplicates topics from a list of destination summaries.
func parseTenantTopics(destinationSummaryList []destinationSummary) []string {
	all := false
	topicsSet := make(map[string]struct{})
	for _, destination := range destinationSummaryList {
		for _, topic := range destination.Topics {
			if topic == "*" {
				all = true
				break
			}
			topicsSet[topic] = struct{}{}
		}
	}

	if all {
		return []string{"*"}
	}

	topics := make([]string, 0, len(topicsSet))
	for topic := range topicsSet {
		topics = append(topics, topic)
	}

	sort.Strings(topics)
	return topics
}

// matchDestinationFilter checks if a destination summary matches the given filter criteria.
func matchDestinationFilter(filter *driver.DestinationFilter, summary destinationSummary) bool {
	if len(filter.Type) > 0 {
		found := false
		for _, t := range filter.Type {
			if t == summary.Type {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(filter.Topics) > 0 {
		filterMatchesAll := len(filter.Topics) == 1 && filter.Topics[0] == "*"
		if !summary.Topics.MatchesAll() {
			if filterMatchesAll {
				return false
			}
			for _, topic := range filter.Topics {
				found := false
				for _, st := range summary.Topics {
					if st == topic {
						found = true
						break
					}
				}
				if !found {
					return false
				}
			}
		}
	}
	return true
}
