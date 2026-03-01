package config

import (
	"strings"

	"go.uber.org/zap"
)

// LogConfigurationSummary returns zap fields with configuration summary, masking sensitive data
//
// ⚠️ IMPORTANT: When adding new configuration fields, you MUST update this function
// to include them in the startup logs. This helps with troubleshooting and ensures
// configuration visibility.
//
// Guidelines:
//   - For non-sensitive fields: use zap.String(), zap.Int(), zap.Bool(), etc.
//   - For sensitive fields (secrets, passwords, keys): use zap.Bool("field_configured", value != "")
//   - For URLs with credentials: use helper functions like maskURL() or maskPostgresURLHost()
//
// See contributing/config.md for detailed guidelines on configuration logging.
func (c *Config) LogConfigurationSummary() []zap.Field {
	fields := []zap.Field{
		// General
		zap.String("service", c.Service),
		zap.String("config_file_path", func() string {
			if c.configPath != "" {
				return c.configPath
			}
			return "none (using defaults and environment variables)"
		}()),
		zap.String("log_level", c.LogLevel),
		zap.Bool("audit_log", c.AuditLog),
		zap.String("deployment_id", c.DeploymentID),
		zap.Strings("topics", c.Topics),
		zap.String("organization_name", c.OrganizationName),
		zap.String("http_user_agent", c.HTTPUserAgent),

		// API
		zap.Int("api_port", c.APIPort),
		zap.Bool("api_key_configured", c.APIKey != ""),
		zap.Bool("api_jwt_secret_configured", c.APIJWTSecret != ""),
		zap.String("gin_mode", c.GinMode),

		// Application
		zap.Bool("aes_encryption_secret_configured", c.AESEncryptionSecret != ""),

		// Redis
		zap.String("redis_host", c.Redis.Host),
		zap.Int("redis_port", c.Redis.Port),
		zap.Bool("redis_password_configured", c.Redis.Password != ""),
		zap.Int("redis_database", c.Redis.Database),
		zap.Bool("redis_tls_enabled", c.Redis.TLSEnabled),
		zap.Bool("redis_cluster_enabled", c.Redis.ClusterEnabled),

		// PostgreSQL
		zap.Bool("postgres_configured", c.PostgresURL != ""),
		zap.String("postgres_host", maskPostgresURLHost(c.PostgresURL)),

		// ClickHouse
		zap.Bool("clickhouse_configured", c.ClickHouse.Addr != ""),
		zap.String("clickhouse_addr", c.ClickHouse.Addr),
		zap.String("clickhouse_database", c.ClickHouse.Database),
		zap.Bool("clickhouse_password_configured", c.ClickHouse.Password != ""),
		zap.Bool("clickhouse_tls_enabled", c.ClickHouse.TLSEnabled),

		// Message Queue
		zap.String("mq_type", c.MQs.GetInfraType()),

		// Consumers
		zap.Int("publish_max_concurrency", c.PublishMaxConcurrency),
		zap.Int("delivery_max_concurrency", c.DeliveryMaxConcurrency),
		zap.Int("log_max_concurrency", c.LogMaxConcurrency),

		// Delivery Retry
		zap.Ints("retry_schedule", c.RetrySchedule),
		zap.Int("retry_interval_seconds", c.RetryIntervalSeconds),
		zap.Int("retry_max_limit", c.RetryMaxLimit),

		// Event Delivery
		zap.Int("max_destinations_per_tenant", c.MaxDestinationsPerTenant),
		zap.Int("delivery_timeout_seconds", c.DeliveryTimeoutSeconds),

		// Idempotency
		zap.Int("publish_idempotency_key_ttl", c.PublishIdempotencyKeyTTL),
		zap.Int("delivery_idempotency_key_ttl", c.DeliveryIdempotencyKeyTTL),

		// Log batcher
		zap.Int("log_batch_threshold_seconds", c.LogBatchThresholdSeconds),
		zap.Int("log_batch_size", c.LogBatchSize),

		// Telemetry
		zap.Bool("telemetry_disabled", c.Telemetry.Disabled || c.DisableTelemetry),

		// Alert
		zap.String("alert_callback_url", maskURL(c.Alert.CallbackURL)),
		zap.Int("alert_consecutive_failure_count", c.Alert.ConsecutiveFailureCount),
		zap.Bool("alert_auto_disable_destination", c.Alert.AutoDisableDestination),

		// ID Generation
		zap.String("idgen_type", c.IDGen.Type),
		zap.String("idgen_event_prefix", c.IDGen.EventPrefix),

		// Retention
		zap.Int("clickhouse_log_retention_ttl_days", c.ClickHouseLogRetentionTTLDays),
	}

	// Add MQ-specific fields based on type
	mqType := c.MQs.GetInfraType()
	fields = append(fields, c.getMQSpecificFields(mqType)...)

	return fields
}

// getMQSpecificFields returns MQ-specific configuration fields
//
// ⚠️ IMPORTANT: When adding new MQ configuration fields, update the appropriate case
// in this function to include them in startup logs.
func (c *Config) getMQSpecificFields(mqType string) []zap.Field {
	switch mqType {
	case "rabbitmq":
		return []zap.Field{
			zap.String("rabbitmq_url", maskURL(c.MQs.RabbitMQ.ServerURL)),
			zap.String("rabbitmq_exchange", c.MQs.RabbitMQ.Exchange),
			zap.String("rabbitmq_delivery_queue", c.MQs.RabbitMQ.DeliveryQueue),
			zap.String("rabbitmq_log_queue", c.MQs.RabbitMQ.LogQueue),
		}
	case "awssqs":
		return []zap.Field{
			zap.Bool("aws_access_key_configured", c.MQs.AWSSQS.AccessKeyID != ""),
			zap.Bool("aws_secret_key_configured", c.MQs.AWSSQS.SecretAccessKey != ""),
			zap.String("aws_region", c.MQs.AWSSQS.Region),
			zap.String("aws_delivery_queue", c.MQs.AWSSQS.DeliveryQueue),
			zap.String("aws_log_queue", c.MQs.AWSSQS.LogQueue),
		}
	case "gcppubsub":
		return []zap.Field{
			zap.Bool("gcp_credentials_configured", c.MQs.GCPPubSub.ServiceAccountCredentials != ""),
			zap.String("gcp_project_id", c.MQs.GCPPubSub.Project),
			zap.String("gcp_delivery_topic", c.MQs.GCPPubSub.DeliveryTopic),
			zap.String("gcp_delivery_subscription", c.MQs.GCPPubSub.DeliverySubscription),
			zap.String("gcp_log_topic", c.MQs.GCPPubSub.LogTopic),
			zap.String("gcp_log_subscription", c.MQs.GCPPubSub.LogSubscription),
		}
	case "azureservicebus":
		return []zap.Field{
			zap.Bool("azure_connection_string_configured", c.MQs.AzureServiceBus.ConnectionString != ""),
			zap.String("azure_delivery_topic", c.MQs.AzureServiceBus.DeliveryTopic),
			zap.String("azure_delivery_subscription", c.MQs.AzureServiceBus.DeliverySubscription),
			zap.String("azure_log_topic", c.MQs.AzureServiceBus.LogTopic),
			zap.String("azure_log_subscription", c.MQs.AzureServiceBus.LogSubscription),
		}
	default:
		return []zap.Field{}
	}
}

// maskURL masks credentials in a URL
func maskURL(url string) string {
	if url == "" {
		return ""
	}
	// Basic masking for URLs with credentials
	// Format: protocol://user:password@host:port
	if idx := strings.Index(url, "://"); idx != -1 {
		protocol := url[:idx+3]
		rest := url[idx+3:]
		if atIdx := strings.Index(rest, "@"); atIdx != -1 {
			host := rest[atIdx:]
			return protocol + "***:***" + host
		}
	}
	return url
}

// maskPostgresURLHost extracts and returns just the host from a postgres URL
func maskPostgresURLHost(url string) string {
	if url == "" {
		return ""
	}

	// postgres://user:password@host:port/database?params
	if idx := strings.Index(url, "@"); idx != -1 {
		rest := url[idx+1:]
		// Get host:port before the database name
		if slashIdx := strings.Index(rest, "/"); slashIdx != -1 {
			return rest[:slashIdx]
		}
		// No database name, get host:port before params
		if qIdx := strings.Index(rest, "?"); qIdx != -1 {
			return rest[:qIdx]
		}
		return rest
	}
	return "not configured"
}
