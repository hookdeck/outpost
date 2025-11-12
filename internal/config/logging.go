package config

import (
	"fmt"
	"reflect"
	"strings"

	"go.uber.org/zap"
)

// LogConfigurationSummary returns zap fields with configuration summary, masking sensitive data
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
		zap.String("alert_callback_url", c.Alert.CallbackURL),
		zap.Int("alert_consecutive_failure_count", c.Alert.ConsecutiveFailureCount),
		zap.Bool("alert_auto_disable_destination", c.Alert.AutoDisableDestination),

		// ID Generation
		zap.String("idgen_type", c.IDGen.Type),
		zap.String("idgen_event_prefix", c.IDGen.EventPrefix),
	}

	// Add MQ-specific fields based on type
	mqType := c.MQs.GetInfraType()
	fields = append(fields, c.getMQSpecificFields(mqType)...)

	return fields
}

// getMQSpecificFields returns MQ-specific configuration fields
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

// LogEnvironmentVariables logs all Outpost-related environment variables
func LogEnvironmentVariables(getenv func(string) string, environ func() []string) []zap.Field {
	envVars := make(map[string]string)
	
	// Get all environment variables
	for _, env := range environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}
		
		key := parts[0]
		value := parts[1]
		
		// Only include Outpost-related env vars (those that are used in config)
		if isOutpostEnvVar(key) {
			// Mask sensitive values
			if isSensitiveEnvVar(key) {
				if value != "" {
					envVars[key] = "***configured***"
				} else {
					envVars[key] = ""
				}
			} else {
				envVars[key] = value
			}
		}
	}
	
	// Convert to zap fields
	fields := []zap.Field{}
	for key, value := range envVars {
		fields = append(fields, zap.String(key, value))
	}
	
	return fields
}

// isOutpostEnvVar checks if an environment variable is related to Outpost configuration
func isOutpostEnvVar(key string) bool {
	outpostPrefixes := []string{
		"SERVICE",
		"LOG_LEVEL",
		"AUDIT_LOG",
		"API_",
		"CONFIG",
		"DEPLOYMENT_ID",
		"AES_ENCRYPTION_SECRET",
		"TOPICS",
		"ORGANIZATION_NAME",
		"HTTP_USER_AGENT",
		"REDIS_",
		"POSTGRES_",
		"CLICKHOUSE_",
		"RABBITMQ_",
		"AWS_",
		"GCP_",
		"GOOGLE_",
		"AZURE_",
		"PUBLISH_",
		"DELIVERY_",
		"LOG_",
		"RETRY_",
		"MAX_",
		"DESTINATION_",
		"IDEMPOTENCY_",
		"TELEMETRY_",
		"DISABLE_TELEMETRY",
		"ALERT_",
		"OTEL_",
		"GIN_MODE",
		"PORTAL_",
		"IDGEN_",
	}
	
	for _, prefix := range outpostPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	
	// Special case for AWS credentials without prefix
	if key == "AWS_ACCESS_KEY_ID" || key == "AWS_SECRET_ACCESS_KEY" || key == "AWS_REGION" {
		return true
	}
	
	return false
}

// isSensitiveEnvVar checks if an environment variable contains sensitive data
func isSensitiveEnvVar(key string) bool {
	sensitiveKeywords := []string{
		"SECRET",
		"PASSWORD",
		"KEY",
		"TOKEN",
		"CREDENTIALS",
		"DSN",
		"URL", // URLs often contain credentials
		"CONNECTION_STRING",
		"RABBITMQ_", // RabbitMQ URL contains credentials
	}
	
	keyUpper := strings.ToUpper(key)
	for _, keyword := range sensitiveKeywords {
		if strings.Contains(keyUpper, keyword) {
			return true
		}
	}
	
	return false
}

// Helper to get field value using reflection
func getFieldValue(v reflect.Value, name string) interface{} {
	field := v.FieldByName(name)
	if !field.IsValid() {
		return nil
	}
	return field.Interface()
}

// Helper to format value, masking if it's a secret field
func formatValue(value interface{}, isSecret bool) string {
	if value == nil {
		return "<nil>"
	}
	
	if isSecret {
		// For string secrets, show if configured or not
		if strVal, ok := value.(string); ok {
			if strVal == "" {
				return "<not configured>"
			}
			return "***configured***"
		}
		return "***configured***"
	}
	
	return fmt.Sprintf("%v", value)
}

