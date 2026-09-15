package mail

import (
	"encoding/json"
	"strings"
	"time"
)

// Defaults aim at the common case: an internal relay on port 25 that accepts
// mail from the network without credentials.
const (
	defaultPort     = 25
	defaultSecurity = "auto"
	defaultTimeout  = 10 * time.Second
	defaultFromName = "jikim"
)

// SettingKey is the settings row that holds everything but the password.
// The field names below it are the standard's `mail.<field>` names, so an
// operator who learned them on another service finds the same ones here.
const (
	SettingKey         = "mail"
	PasswordSettingKey = "mail_password"
)

// EventSettings maps each event to the switch that silences it. Two events
// that answer the same question (was my request decided?) share a switch.
var EventSettings = map[string]string{
	EventApprovalRequested: "notify_approval_request",
	EventApprovalDecided:   "notify_approval_decision",
	EventRotationFailed:    "notify_rotation_failed",
}

// EventSwitches lists the switch names in a stable order for the settings
// screen and its validation.
func EventSwitches() []string {
	return []string{"notify_approval_request", "notify_approval_decision", "notify_rotation_failed"}
}

// ReadConfig builds the configuration from the stored `mail` setting and the
// separately encrypted password. Missing values take the defaults, so a fresh
// install reads as "off" and a relay on port 25 needs nothing but a host.
func ReadConfig(values map[string]any, password string) Config {
	config := Config{Port: defaultPort, Security: defaultSecurity, Timeout: defaultTimeout, FromName: defaultFromName, Events: map[string]bool{}}
	config.Password = password
	if values == nil {
		return config
	}
	config.Enabled, _ = values["enabled"].(bool)
	config.Host = stringValue(values, "smtp_host", "")
	config.Username = stringValue(values, "username", "")
	config.FromAddress = stringValue(values, "from_address", "")
	config.FromName = stringValue(values, "from_name", defaultFromName)
	config.Security = strings.ToLower(stringValue(values, "security", defaultSecurity))
	config.BaseURL = stringValue(values, "base_url", "")
	config.SkipVerify, _ = values["skip_tls_verify"].(bool)
	if port, ok := numberValue(values, "smtp_port"); ok && port > 0 {
		config.Port = port
	}
	if seconds, ok := numberValue(values, "timeout_seconds"); ok && seconds > 0 {
		config.Timeout = time.Duration(seconds) * time.Second
	}
	// A relay on the implicit TLS port needs no extra configuration.
	if config.Security == defaultSecurity && config.Port == 465 {
		config.Security = "tls"
	}
	for event, key := range EventSettings {
		if enabled, ok := values[key].(bool); ok {
			config.Events[event] = enabled
		}
	}
	if strings.TrimSpace(config.FromAddress) == "" && strings.TrimSpace(config.Host) != "" {
		config.FromAddress = "jikim@" + config.Host
	}
	return config
}

func stringValue(values map[string]any, key, fallback string) string {
	if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func numberValue(values map[string]any, key string) (int, bool) {
	switch typed := values[key].(type) {
	case float64:
		return int(typed), true
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case json.Number:
		value, err := typed.Int64()
		return int(value), err == nil
	}
	return 0, false
}
