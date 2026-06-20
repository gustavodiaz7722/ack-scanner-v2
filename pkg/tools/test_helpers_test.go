package tools

import "strings"

// containsSubstr checks if s contains the given substring.
func containsSubstr(s, substr string) bool {
	return strings.Contains(s, substr)
}

// contains checks if s contains the given substring (alias for containsSubstr).
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
