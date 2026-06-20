package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// TestProperty_ModelFilenameExtractionRoundTrip verifies that for any service
// name (lowercase letters and hyphens, 2-30 chars, starting/ending with a
// letter, no consecutive hyphens), constructing a filename as {service}.json
// and then extracting the service name SHALL recover the original value.
//
// **Validates: Requirements 3.3**
func TestProperty_ModelFilenameExtractionRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate service name: lowercase letters with optional hyphens between segments
		// Pattern: segment(-segment)* where segment is lowercase letters 1-10 chars
		numSegments := rapid.IntRange(1, 4).Draw(t, "numSegments")
		segments := make([]string, numSegments)
		for i := range segments {
			segLen := rapid.IntRange(1, 10).Draw(t, "segLen")
			segBytes := make([]byte, segLen)
			for j := range segBytes {
				segBytes[j] = byte(rapid.IntRange('a', 'z').Draw(t, "segByte"))
			}
			segments[i] = string(segBytes)
		}
		serviceName := strings.Join(segments, "-")

		// Construct filename
		filename := serviceName + ".json"

		// Extract and verify round-trip
		got := ExtractModelServiceName(filename)
		if got == "" {
			t.Fatalf("extraction returned empty for filename %q (service=%q)", filename, serviceName)
		}
		if got != serviceName {
			t.Fatalf("service mismatch: got %q, want %q (filename=%q)", got, serviceName, filename)
		}
	})
}

// TestProperty_ModelFilenameExtractionRejectsInvalid verifies that filenames
// without the .json suffix or with empty base names are rejected.
//
// **Validates: Requirements 3.3**
func TestProperty_ModelFilenameExtractionRejectsInvalid(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random non-.json suffix
		suffixLen := rapid.IntRange(1, 10).Draw(t, "suffixLen")
		suffixBytes := make([]byte, suffixLen)
		for i := range suffixBytes {
			suffixBytes[i] = byte(rapid.IntRange('a', 'z').Draw(t, "suffixByte"))
		}
		suffix := "." + string(suffixBytes)

		// Ensure suffix is NOT ".json"
		if suffix == ".json" {
			suffix = ".yaml"
		}

		// Generate a base name
		baseLen := rapid.IntRange(1, 20).Draw(t, "baseLen")
		baseBytes := make([]byte, baseLen)
		for i := range baseBytes {
			baseBytes[i] = byte(rapid.IntRange('a', 'z').Draw(t, "baseByte"))
		}
		filename := string(baseBytes) + suffix

		// Extraction should return empty string for non-.json files
		got := ExtractModelServiceName(filename)
		if got != "" {
			t.Fatalf("expected empty for non-.json filename %q, got %q", filename, got)
		}
	})
}

// TestProperty_ModelDiscoveryJSONOutputValidity verifies that for any discovery
// result, serializing to JSON SHALL produce valid JSON where every entry
// contains service_name and file_path fields with non-empty values.
//
// **Validates: Requirements 3.4, 3.5**
func TestProperty_ModelDiscoveryJSONOutputValidity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random DiscoverModelsOutput
		numModels := rapid.IntRange(0, 20).Draw(t, "numModels")
		models := make([]APIModelInfo, numModels)
		for i := range models {
			// Generate service name with optional hyphens
			numSegments := rapid.IntRange(1, 4).Draw(t, "numSegments")
			segments := make([]string, numSegments)
			for j := range segments {
				segLen := rapid.IntRange(1, 10).Draw(t, "segLen")
				segBytes := make([]byte, segLen)
				for k := range segBytes {
					segBytes[k] = byte(rapid.IntRange('a', 'z').Draw(t, "segByte"))
				}
				segments[j] = string(segBytes)
			}
			serviceName := strings.Join(segments, "-")

			models[i] = APIModelInfo{
				ServiceName: serviceName,
				FilePath:    "codegen/sdk-codegen/aws-models/" + serviceName + ".json",
			}
		}

		output := &DiscoverModelsOutput{
			Models: models,
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

		// Verify "models" key exists
		modelsRaw, ok := parsed["models"]
		if !ok {
			t.Fatal("JSON output missing 'models' key")
		}

		// Parse models array
		var entries []map[string]interface{}
		if err := json.Unmarshal(modelsRaw, &entries); err != nil {
			t.Fatalf("'models' is not a valid JSON array: %v", err)
		}

		// Verify each entry has the required fields
		if len(entries) != numModels {
			t.Fatalf("expected %d entries, got %d", numModels, len(entries))
		}

		for i, entry := range entries {
			if _, ok := entry["service_name"]; !ok {
				t.Fatalf("entry %d missing 'service_name'", i)
			}
			if _, ok := entry["file_path"]; !ok {
				t.Fatalf("entry %d missing 'file_path'", i)
			}

			// Verify values are non-empty strings
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
