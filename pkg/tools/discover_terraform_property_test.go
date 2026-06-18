package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aws-controllers-k8s/ack-scanner-v2/pkg/types"
	"pgregory.net/rapid"
)

// TestProperty5_TerraformFilenameExtractionRoundTrip verifies that for any
// service name (lowercase letters, 2-12 chars) and resource name (snake_case,
// 1-3 segments), constructing a filename as {service}_{resource}.html.markdown
// and then extracting service and resource SHALL recover the original values.
//
// **Validates: Requirements 2.3**
func TestProperty5_TerraformFilenameExtractionRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate service name: lowercase letters, 2-12 chars
		serviceLen := rapid.IntRange(2, 12).Draw(t, "serviceLen")
		serviceBytes := make([]byte, serviceLen)
		for i := range serviceBytes {
			serviceBytes[i] = byte(rapid.IntRange('a', 'z').Draw(t, "serviceByte"))
		}
		service := string(serviceBytes)

		// Generate resource name: snake_case, 1-3 segments
		// Each segment is lowercase letters, 1-10 chars
		numSegments := rapid.IntRange(1, 3).Draw(t, "numSegments")
		segments := make([]string, numSegments)
		for i := range segments {
			segLen := rapid.IntRange(1, 10).Draw(t, "segLen")
			segBytes := make([]byte, segLen)
			for j := range segBytes {
				segBytes[j] = byte(rapid.IntRange('a', 'z').Draw(t, "segByte"))
			}
			segments[i] = string(segBytes)
		}
		resource := strings.Join(segments, "_")

		// Construct filename
		filename := service + "_" + resource + ".html.markdown"

		// Extract and verify round-trip
		gotService, gotResource, ok := ExtractTerraformFilenameComponents(filename)
		if !ok {
			t.Fatalf("extraction failed for filename %q (service=%q, resource=%q)", filename, service, resource)
		}
		if gotService != service {
			t.Fatalf("service mismatch: got %q, want %q (filename=%q)", gotService, service, filename)
		}
		if gotResource != resource {
			t.Fatalf("resource mismatch: got %q, want %q (filename=%q)", gotResource, resource, filename)
		}
	})
}

// TestProperty6_TerraformDiscoveryJSONOutputValidity verifies that for any
// discovery result, serializing to JSON SHALL produce valid JSON where every
// entry contains service_name, resource_type, and doc_file_path.
//
// **Validates: Requirements 2.4, 2.5**
func TestProperty6_TerraformDiscoveryJSONOutputValidity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random DiscoverTerraformOutput
		numResources := rapid.IntRange(0, 20).Draw(t, "numResources")
		resources := make([]types.TerraformResourceInfo, numResources)
		for i := range resources {
			serviceLen := rapid.IntRange(2, 12).Draw(t, "serviceLen")
			serviceBytes := make([]byte, serviceLen)
			for j := range serviceBytes {
				serviceBytes[j] = byte(rapid.IntRange('a', 'z').Draw(t, "serviceByte"))
			}

			resourceSegments := rapid.IntRange(1, 3).Draw(t, "numSegments")
			segs := make([]string, resourceSegments)
			for j := range segs {
				segLen := rapid.IntRange(1, 10).Draw(t, "segLen")
				segBytes := make([]byte, segLen)
				for k := range segBytes {
					segBytes[k] = byte(rapid.IntRange('a', 'z').Draw(t, "segByte"))
				}
				segs[j] = string(segBytes)
			}
			resourceType := strings.Join(segs, "_")

			resources[i] = types.TerraformResourceInfo{
				ServiceName:  string(serviceBytes),
				ResourceType: resourceType,
				DocFilePath:  "website/docs/r/" + string(serviceBytes) + "_" + resourceType + ".html.markdown",
			}
		}

		output := &DiscoverTerraformOutput{
			Resources: resources,
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

		// Verify "resources" key exists
		resourcesRaw, ok := parsed["resources"]
		if !ok {
			t.Fatal("JSON output missing 'resources' key")
		}

		// Parse resources array
		var entries []map[string]interface{}
		if err := json.Unmarshal(resourcesRaw, &entries); err != nil {
			t.Fatalf("'resources' is not a valid JSON array: %v", err)
		}

		// Verify each entry has the required fields
		if len(entries) != numResources {
			t.Fatalf("expected %d entries, got %d", numResources, len(entries))
		}

		for i, entry := range entries {
			if _, ok := entry["service_name"]; !ok {
				t.Fatalf("entry %d missing 'service_name'", i)
			}
			if _, ok := entry["resource_type"]; !ok {
				t.Fatalf("entry %d missing 'resource_type'", i)
			}
			if _, ok := entry["doc_file_path"]; !ok {
				t.Fatalf("entry %d missing 'doc_file_path'", i)
			}

			// Verify values are non-empty strings
			sn, ok := entry["service_name"].(string)
			if !ok || sn == "" {
				t.Fatalf("entry %d: 'service_name' is empty or not a string", i)
			}
			rt, ok := entry["resource_type"].(string)
			if !ok || rt == "" {
				t.Fatalf("entry %d: 'resource_type' is empty or not a string", i)
			}
			dfp, ok := entry["doc_file_path"].(string)
			if !ok || dfp == "" {
				t.Fatalf("entry %d: 'doc_file_path' is empty or not a string", i)
			}
		}
	})
}
