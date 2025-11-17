# Config

This document provides guidelines for working with Outpost configuration.

## Adding New Configuration Fields

When adding new configuration fields to Outpost, follow these steps to ensure consistency and proper logging:

### 1. Define the Configuration Field

Add your new field to the appropriate config struct in `internal/config/`:

```go
type Config struct {
    // ... existing fields ...
    MyNewField string `yaml:"my_new_field" env:"MY_NEW_FIELD" desc:"Description of the field" required:"N"`
}
```

### 2. Add Default Values (if applicable)

Update `InitDefaults()` in `internal/config/config.go`:

```go
func (c *Config) InitDefaults() {
    // ... existing defaults ...
    c.MyNewField = "default_value"
}
```

### 3. Update Configuration Logging ⚠️ IMPORTANT

**To maintain visibility into startup configuration, you MUST update the configuration logging helper** in `internal/config/logging.go`:

#### For General Configuration Fields

Add your field to `LogConfigurationSummary()`:

```go
func (c *Config) LogConfigurationSummary() []zap.Field {
    fields := []zap.Field{
        // ... existing fields ...
        
        // For non-sensitive fields:
        zap.String("my_new_field", c.MyNewField),
        
        // For sensitive fields (passwords, secrets, keys):
        zap.Bool("my_secret_field_configured", c.MySecretField != ""),
        
        // ... rest of fields ...
    }
    return fields
}
```

#### For Message Queue Configuration

If adding MQ-specific fields, update `getMQSpecificFields()`:

```go
func (c *Config) getMQSpecificFields(mqType string) []zap.Field {
    switch mqType {
    case "rabbitmq":
        return []zap.Field{
            // ... existing fields ...
            zap.String("rabbitmq_my_field", c.MQs.RabbitMQ.MyField),
        }
    // ... other cases ...
    }
}
```

#### For Sensitive Environment Variables

If your field contains sensitive data (passwords, secrets, API keys, tokens, URLs with credentials), update `isSensitiveEnvVar()`:

```go
func isSensitiveEnvVar(key string) bool {
    sensitiveKeywords := []string{
        // ... existing keywords ...
        "MY_SENSITIVE_KEYWORD", // Add if needed
    }
    // ... rest of function ...
}
```

### 4. Guidelines for Sensitive Data

**Always mask sensitive data in logs:**

- ✅ **DO**: Use `zap.Bool("field_configured", value != "")` for secrets
- ✅ **DO**: Use helper functions like `maskURL()` for URLs with credentials
- ❌ **DON'T**: Log actual passwords, API keys, tokens, or secrets
- ❌ **DON'T**: Log full connection strings with credentials

**Examples:**

```go
// Good - shows if configured without exposing value
zap.Bool("api_key_configured", c.APIKey != "")

// Good - masks credentials in URL
zap.String("database_url", maskPostgresURLHost(c.PostgresURL))

// Bad - exposes sensitive data
zap.String("api_key", c.APIKey) // ❌ NEVER DO THIS
```

### 5. Update Validation (if needed)

If your field requires validation, update `Validate()` in `internal/config/validation.go`.

### 6. Update Documentation

Don't forget to regenerate the configuration documentation:

```bash
go generate ./internal/config/...
```

This will update `docs/pages/references/configuration.mdx` with your new field's description.

## Configuration Logging Checklist

When adding or modifying configuration fields, use this checklist:

- [ ] Field added to appropriate struct with `yaml`, `env`, `desc`, and `required` tags
- [ ] Default value added to `InitDefaults()` (if applicable)
- [ ] **Field added to `LogConfigurationSummary()` in `internal/config/logging.go`**
- [ ] **Sensitive fields are masked (showing only if configured, not actual value)**
- [ ] MQ-specific fields added to `getMQSpecificFields()` (if applicable)
- [ ] Environment variable keywords added to `isOutpostEnvVar()` (if new prefix)
- [ ] Sensitive keywords added to `isSensitiveEnvVar()` (if contains secrets)
- [ ] Validation added (if required)
- [ ] Documentation regenerated with `go generate`
- [ ] Changes tested with `LOG_LEVEL=info` to verify logs appear correctly

## Why Configuration Logging Matters

Configuration logging serves several critical purposes:

1. **Troubleshooting**: When users report issues, configuration logs help identify misconfiguration quickly
2. **Security Auditing**: Shows what's configured without exposing sensitive values
3. **Deployment Verification**: Confirms the application started with expected configuration
4. **Documentation**: Provides a real-world example of what configuration is being used

Keeping configuration logging up-to-date prevents "configuration drift" where the code and logs don't match, making troubleshooting harder.

## MQs

TBD

## OpenTelemetry

To support OpenTelemetry, you must have this env `OTEL_SERVICE_NAME`. Its value is your service name when sending to OTEL. You can use whichever value you see fit.

In production, if Outpost is run as a singular service, then the service name can be `outpost`. If Outpost is run in multiple processes (for API, delivery, log, etc.), you can provide more granularity by including the service type such as `outpost.api` or `outpost.delivery`, etc. Ultimately, it's up to the end users which value they want to see in their telemetry data.

Besides `OTEL_SERVICE_NAME`, we support the official [OpenTelemetry Environment Variable Specification](https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/).

To specify the exporter endpoint, you can use `OTEL_EXPORTER_OTLP_ENDPOINT` or individual exporters such as `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`, `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT`, or `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT`.

By default, Outpost will export all Telemetry data. You can disable specific telemetry by setting its exporter to `none`. For example, if you only want to receive traces & metrics:

```
OTEL_TRACES_EXPORTER="otlp" # default
OTEL_METRICS_EXPORTER="otlp" # default
OTEL_LOGS_EXPORTER="none" # disable logs
```

Currently, we only support `otlp` exporter. If you have specific needs for other exporter configuration (like Prometheus), you must set up your own OTEL collector and configure it accordingly.
