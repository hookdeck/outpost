package config

import (
	"testing"
)

// TestRedisClusterConfigDefaults ensures backward compatibility by testing
// that REDIS_CLUSTER_ENABLED defaults to false and doesn't break existing deployments
func TestRedisClusterConfigDefaults(t *testing.T) {
	t.Run("ClusterEnabledDefaultsToFalse", func(t *testing.T) {
		config := RedisConfig{}
		if config.ClusterEnabled {
			t.Error("ClusterEnabled should default to false for backward compatibility")
		}
	})

	t.Run("ToConfigPreservesClusterEnabled", func(t *testing.T) {
		// Test with cluster disabled (default)
		config := RedisConfig{
			Host:           "localhost",
			Port:           6379,
			Password:       "test",
			Database:       0,
			TLSEnabled:     false,
			ClusterEnabled: false,
		}
		
		redisConfig := config.ToConfig()
		if redisConfig.ClusterEnabled {
			t.Error("ToConfig() should preserve ClusterEnabled=false")
		}

		// Test with cluster enabled
		config.ClusterEnabled = true
		redisConfig = config.ToConfig()
		if !redisConfig.ClusterEnabled {
			t.Error("ToConfig() should preserve ClusterEnabled=true")
		}
	})

	t.Run("BackwardCompatibilityWithoutClusterField", func(t *testing.T) {
		// Simulate old configuration without ClusterEnabled field
		config := RedisConfig{
			Host:       "localhost",
			Port:       6379,
			Password:   "test",
			Database:   0,
			TLSEnabled: false,
			// ClusterEnabled is not set, should default to false
		}

		redisConfig := config.ToConfig()
		if redisConfig.ClusterEnabled {
			t.Error("Configuration without ClusterEnabled should default to false")
		}
	})
}