package migrator

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// sanitizeConnectionError takes an error from the migrate library and returns
// a sanitized version that removes any credentials while preserving useful context.
//
// The migrate library includes the full database URL in its error messages,
// which can expose passwords in logs. This function:
// 1. Attempts to remove/redact detected credentials from the error
// 2. If credentials can't be reliably detected, redacts the entire URL
func sanitizeConnectionError(err error, dbURL string) error {
	if err == nil {
		return nil
	}

	errMsg := err.Error()
	
	// If the error contains the database URL, replace it entirely
	// This is the safest approach - better to lose some context than leak credentials
	if dbURL != "" && strings.Contains(errMsg, dbURL) {
		// Try to parse and create a safe version first
		if u, err := url.Parse(dbURL); err == nil && u != nil && u.Host != "" {
			// Create a sanitized version with just scheme and host
			safeURL := fmt.Sprintf("%s://[REDACTED]@%s/[REDACTED]", u.Scheme, u.Host)
			errMsg = strings.ReplaceAll(errMsg, dbURL, safeURL)
		} else {
			// Can't parse - just replace with a generic message
			errMsg = strings.ReplaceAll(errMsg, dbURL, "[DATABASE_URL_REDACTED]")
		}
	}
	
	// Also try to remove any other credential patterns
	sanitized := removeCredentialsFromError(errMsg, dbURL)
	
	// Return the sanitized error wrapped with context
	return fmt.Errorf("migrate.New: %s", sanitized)
}

// removeCredentialsFromError removes any credentials found in the error message.
// It parses the database URL to identify credentials and replaces them in the error string.
func removeCredentialsFromError(errMsg string, dbURL string) string {
	if dbURL == "" {
		return errMsg
	}

	// Parse the URL to extract credentials
	u, err := url.Parse(dbURL)
	// Also check if it's actually a database URL (has scheme and user info)
	if err != nil || u == nil || u.Scheme == "" || u.User == nil {
		// If we can't parse the URL, still try to redact it using string matching
		// This handles malformed URLs in parse errors
		result := errMsg
		
		// Try to extract username:password pattern from the malformed URL
		if idx := strings.Index(dbURL, "://"); idx >= 0 {
			afterScheme := dbURL[idx+3:]
			if atIdx := strings.Index(afterScheme, "@"); atIdx >= 0 {
				userInfo := afterScheme[:atIdx]
				if colonIdx := strings.Index(userInfo, ":"); colonIdx >= 0 {
					username := userInfo[:colonIdx]
					password := userInfo[colonIdx+1:]
					
					// Replace the password wherever it appears
					result = strings.ReplaceAll(result, password, "[REDACTED]")
					// Replace user:password pattern
					result = strings.ReplaceAll(result, userInfo, username+":[REDACTED]")
				}
			}
		}
		
		// Also apply common patterns
		return removeCommonCredentialPatterns(result)
	}

	result := errMsg

	// Remove the full URL if it appears in the error (including quoted versions)
	if strings.Contains(result, dbURL) {
		// Replace with a sanitized version
		sanitizedURL := sanitizeURL(u)
		result = strings.ReplaceAll(result, dbURL, sanitizedURL)
		// Also replace quoted versions
		result = strings.ReplaceAll(result, `"`+dbURL+`"`, `"`+sanitizedURL+`"`)
		result = strings.ReplaceAll(result, `'`+dbURL+`'`, `'`+sanitizedURL+`'`)
	}

	// Remove password if it appears separately
	if u.User != nil {
		if pass, hasPass := u.User.Password(); hasPass && pass != "" {
			// Replace password occurrences
			result = strings.ReplaceAll(result, pass, "[REDACTED]")
			
			// Also replace common patterns like "user:password"
			if username := u.User.Username(); username != "" {
				credPattern := username + ":" + pass
				result = strings.ReplaceAll(result, credPattern, username+":[REDACTED]")
			}
		}
		
		// Replace the entire user info if it appears
		userInfo := u.User.String()
		if userInfo != "" && strings.Contains(result, userInfo) {
			sanitizedUser := u.User.Username()
			if sanitizedUser != "" {
				sanitizedUser += ":[REDACTED]"
			}
			result = strings.ReplaceAll(result, userInfo, sanitizedUser)
		}
	}

	// Remove URL-encoded versions of credentials
	if u.User != nil {
		if pass, hasPass := u.User.Password(); hasPass && pass != "" {
			encodedPass := url.QueryEscape(pass)
			if encodedPass != pass {
				result = strings.ReplaceAll(result, encodedPass, "[REDACTED]")
			}
		}
	}

	return result
}

// sanitizeURL creates a sanitized version of a URL with credentials redacted
func sanitizeURL(u *url.URL) string {
	if u == nil {
		return ""
	}

	// Build the URL manually to avoid URL encoding of [REDACTED]
	var result strings.Builder
	result.WriteString(u.Scheme)
	result.WriteString("://")
	
	if u.User != nil {
		username := u.User.Username()
		if username != "" {
			result.WriteString(username)
			result.WriteString(":[REDACTED]")
			result.WriteString("@")
		}
	}
	
	result.WriteString(u.Host)
	result.WriteString(u.Path)
	if u.RawQuery != "" {
		result.WriteString("?")
		result.WriteString(u.RawQuery)
	}
	if u.Fragment != "" {
		result.WriteString("#")
		result.WriteString(u.Fragment)
	}
	
	return result.String()
}

// removeCommonCredentialPatterns removes common credential patterns when we can't parse the URL
func removeCommonCredentialPatterns(errMsg string) string {
	result := errMsg
	
	// Common patterns to redact - be more aggressive with matching
	patterns := []struct {
		regex   string
		replace string
	}{
		// user:password@ pattern (match any non-whitespace for password)
		{`(\b\w+):([^@\s]+)@`, "$1:[REDACTED]@"},
		// password=value in query strings
		{`password=([^&\s]+)`, "password=[REDACTED]"},
		// common password fields in errors
		{`"password":\s*"[^"]*"`, `"password":"[REDACTED]"`},
		{`'password':\s*'[^']*'`, `'password':'[REDACTED]'`},
		// Password after colon in URLs
		{`://([^:]+):([^@]+)@`, "://$1:[REDACTED]@"},
	}
	
	for _, p := range patterns {
		re := regexp.MustCompile(p.regex)
		result = re.ReplaceAllString(result, p.replace)
	}
	
	return result
}

// extractHostPort extracts the host and port from a database URL without exposing credentials.
// Returns "unknown" for host and "unknown" for port if parsing fails.
func extractHostPort(dbURL string) (string, string) {
	if dbURL == "" {
		return "unknown", "unknown"
	}

	// Try to parse as URL
	u, err := url.Parse(dbURL)
	if err != nil {
		return "unknown", "unknown"
	}

	host := u.Hostname()
	if host == "" {
		host = "unknown"
	}

	port := u.Port()
	if port == "" {
		// Default ports based on scheme
		switch u.Scheme {
		case "postgres", "postgresql":
			port = "5432"
		case "clickhouse":
			port = "9000"
		case "mysql":
			port = "3306"
		default:
			port = "unknown"
		}
	}

	return host, port
}