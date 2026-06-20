package tools

import (
	"encoding/json"
	"testing"

	"pgregory.net/rapid"
)

// TestProperty_UpjetServiceNameExtractionRoundTrip verifies that for any valid
// service name (lowercase letters and digits, 2-20 chars), constructing a path
// as config/<service>/config.go and then extracting the service name SHALL
// recover the original value.
//
// **Validates: Requirements 2.3**
func TestProperty_UpjetServiceNameExtractionRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate service name: lowercase letters and digits, 2-20 chars
		serviceLen := rapid.IntRange(2, 20).Draw(t, "serviceLen")
		serviceBytes := make([]byte, serviceLen)
		for i := range serviceBytes {
			// lowercase letters and digits (like real Upjet services: elasticache, s3, ec2, etc.)
			if rapid.IntRange(0, 4).Draw(t, "charType") == 0 {
				serviceBytes[i] = byte(rapid.IntRange('0', '9').Draw(t, "digitByte"))
			} else {
				serviceBytes[i] = byte(rapid.IntRange('a', 'z').Draw(t, "letterByte"))
			}
		}
		service := string(serviceBytes)

		// Construct path in expected format
		path := "config/" + service + "/config.go"

		// Extract and verify round-trip
		got := ExtractUpjetServiceName(path)
		if got == "" {
			t.Fatalf("extraction returned empty for path %q (service=%q)", path, service)
		}
		if got != service {
			t.Fatalf("service mismatch: got %q, want %q (path=%q)", got, service, path)
		}
	})
}

// TestProperty_UpjetServiceNameExtractionRejectsInvalidPaths verifies that
// paths not matching the config/<service>/config.go pattern SHALL return empty
// string, indicating the path should be skipped.
//
// **Validates: Requirements 2.2, 2.3**
func TestProperty_UpjetServiceNameExtractionRejectsInvalidPaths(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate various invalid path patterns
		pathType := rapid.IntRange(0, 5).Draw(t, "pathType")
		var path string

		switch pathType {
		case 0:
			// Missing config/ prefix
			service := genUpjetServiceName(t)
			path = "other/" + service + "/config.go"
		case 1:
			// Missing /config.go suffix
			service := genUpjetServiceName(t)
			path = "config/" + service + "/main.go"
		case 2:
			// Nested too deeply: config/a/b/config.go
			service1 := genUpjetServiceName(t)
			service2 := genUpjetServiceName(t)
			path = "config/" + service1 + "/" + service2 + "/config.go"
		case 3:
			// No service directory: config/config.go
			path = "config/config.go"
		case 4:
			// Empty service: config//config.go
			path = "config//config.go"
		case 5:
			// Random string with no structure
			strLen := rapid.IntRange(1, 30).Draw(t, "strLen")
			strBytes := make([]byte, strLen)
			for i := range strBytes {
				strBytes[i] = byte(rapid.IntRange('a', 'z').Draw(t, "randByte"))
			}
			path = string(strBytes)
		}

		got := ExtractUpjetServiceName(path)
		if got != "" {
			t.Fatalf("expected empty for invalid path %q, got %q", path, got)
		}
	})
}

// TestProperty_UpjetDiscoveryJSONOutputValidity verifies that for any
// discovery result, serializing to JSON SHALL produce valid JSON where every
// entry contains service_name and file_path.
//
// **Validates: Requirements 2.4, 2.5**
func TestProperty_UpjetDiscoveryJSONOutputValidity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random DiscoverUpjetOutput
		numConfigs := rapid.IntRange(0, 20).Draw(t, "numConfigs")
		configs := make([]UpjetConfigInfo, numConfigs)
		for i := range configs {
			service := genUpjetServiceName(t)
			configs[i] = UpjetConfigInfo{
				ServiceName: service,
				FilePath:    "config/" + service + "/config.go",
			}
		}

		output := &DiscoverUpjetOutput{
			Configs: configs,
		}

		// Serialize to JSON
		data, err := json.Marshal(output)
		if err != nil {
			t.Fatalf("failed to marshal output: %v", err)
		}

		// Verify it's valid JSON
		var parsed map[string]json.RawMessage
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("output is not valid JSON: %v", err)
		}

		// Verify "configs" key exists
		configsRaw, ok := parsed["configs"]
		if !ok {
			t.Fatal("JSON output missing 'configs' key")
		}

		// Parse configs array
		var entries []map[string]any
		if err := json.Unmarshal(configsRaw, &entries); err != nil {
			t.Fatalf("'configs' is not a valid JSON array: %v", err)
		}

		// Verify count matches
		if len(entries) != numConfigs {
			t.Fatalf("expected %d entries, got %d", numConfigs, len(entries))
		}

		// Verify each entry has required fields with non-empty values
		for i, entry := range entries {
			sn, ok := entry["service_name"].(string)
			if !ok || sn == "" {
				t.Fatalf("entry %d: 'service_name' is empty or not a string", i)
			}
			fp, ok := entry["file_path"].(string)
			if !ok || fp == "" {
				t.Fatalf("entry %d: 'file_path' is empty or not a string", i)
			}
		}
	})
}

// genUpjetServiceName generates a random service name (lowercase letters, 2-15 chars)
// for use in Upjet property tests.
func genUpjetServiceName(t *rapid.T) string {
	serviceLen := rapid.IntRange(2, 15).Draw(t, "serviceLen")
	serviceBytes := make([]byte, serviceLen)
	for i := range serviceBytes {
		serviceBytes[i] = byte(rapid.IntRange('a', 'z').Draw(t, "serviceByte"))
	}
	return string(serviceBytes)
}
