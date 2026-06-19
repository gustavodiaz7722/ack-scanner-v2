// Package ignore provides a static configuration-based mechanism for excluding
// fields from the gap report. Fields can be excluded because they are inside
// lists (unsupported), are not actually JSON documents (scanner false positives),
// or for other reasons.
package ignore

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the ignore configuration loaded from a YAML file.
type Config struct {
	// IgnoredFields is a list of field entries to exclude from the gap report.
	IgnoredFields []IgnoredField `yaml:"ignored_fields"`
}

// IgnoredField represents a single field to ignore in the gap report.
type IgnoredField struct {
	// Service is the ACK service name (e.g., "iam", "autoscaling").
	Service string `yaml:"service"`
	// Resource is the CRD resource kind (e.g., "Group", "AutoScalingGroup").
	Resource string `yaml:"resource"`
	// Field is the ACK field name (e.g., "policies", "notificationMetadata").
	Field string `yaml:"field"`
	// Reason explains why this field is ignored.
	Reason string `yaml:"reason"`
}

// key returns a lookup key for the ignored field.
func (f IgnoredField) key() string {
	return f.Service + "/" + f.Resource + "/" + f.Field
}

// List is a lookup structure for efficiently checking if a field should be ignored.
type List struct {
	entries map[string]string // key → reason
}

// LoadConfig reads an ignore configuration from a YAML file.
// If the file does not exist, an empty config is returned (no error).
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("reading ignore config: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing ignore config: %w", err)
	}

	return &config, nil
}

// NewList creates a List from the given config.
func NewList(config *Config) *List {
	l := &List{
		entries: make(map[string]string),
	}
	if config == nil {
		return l
	}
	for _, f := range config.IgnoredFields {
		l.entries[f.key()] = f.Reason
	}
	return l
}

// IsIgnored checks if a specific field should be excluded from the report.
func (l *List) IsIgnored(service, resource, field string) bool {
	if l == nil {
		return false
	}
	key := service + "/" + resource + "/" + field
	_, ok := l.entries[key]
	return ok
}

// Count returns the number of entries in the ignore list.
func (l *List) Count() int {
	if l == nil {
		return 0
	}
	return len(l.entries)
}
