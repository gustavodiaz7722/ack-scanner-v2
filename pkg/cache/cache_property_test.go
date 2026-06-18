package cache

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// toolNameGen generates valid tool names (lowercase letters, underscores, 1-30 chars).
func toolNameGen() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		chars := []byte("abcdefghijklmnopqrstuvwxyz_")
		length := rapid.IntRange(1, 30).Draw(t, "len")
		name := make([]byte, length)
		for i := range name {
			name[i] = chars[rapid.IntRange(0, len(chars)-1).Draw(t, "char")]
		}
		// Ensure doesn't start or end with underscore and no double underscores
		if name[0] == '_' {
			name[0] = 'a'
		}
		if name[len(name)-1] == '_' {
			name[len(name)-1] = 'z'
		}
		return string(name)
	})
}

// itemKeyGen generates valid item keys (lowercase letters, digits, hyphens, 1-30 chars).
func itemKeyGen() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		chars := []byte("abcdefghijklmnopqrstuvwxyz0123456789-")
		length := rapid.IntRange(1, 30).Draw(t, "len")
		key := make([]byte, length)
		for i := range key {
			key[i] = chars[rapid.IntRange(0, len(chars)-1).Draw(t, "char")]
		}
		// Ensure doesn't start or end with hyphen
		if key[0] == '-' {
			key[0] = 'a'
		}
		if key[len(key)-1] == '-' {
			key[len(key)-1] = 'z'
		}
		return string(key)
	})
}

// inputParamsGen generates arbitrary input parameters as a map.
func inputParamsGen() *rapid.Generator[map[string]string] {
	return rapid.Custom(func(t *rapid.T) map[string]string {
		count := rapid.IntRange(0, 5).Draw(t, "param_count")
		params := make(map[string]string, count)
		for i := 0; i < count; i++ {
			key := rapid.StringMatching(`[a-z]{1,10}`).Draw(t, "param_key")
			val := rapid.StringMatching(`[a-zA-Z0-9]{0,20}`).Draw(t, "param_val")
			params[key] = val
		}
		return params
	})
}

// resultJSONGen generates arbitrary valid JSON result data.
func resultJSONGen() *rapid.Generator[json.RawMessage] {
	return rapid.Custom(func(t *rapid.T) json.RawMessage {
		// Generate a simple JSON object with random fields
		obj := make(map[string]any)
		count := rapid.IntRange(1, 5).Draw(t, "field_count")
		for i := 0; i < count; i++ {
			key := rapid.StringMatching(`[a-z]{1,8}`).Draw(t, "field_key")
			val := rapid.StringMatching(`[a-zA-Z0-9 ]{0,30}`).Draw(t, "field_val")
			obj[key] = val
		}
		data, err := json.Marshal(obj)
		if err != nil {
			t.Fatal(err)
		}
		return json.RawMessage(data)
	})
}

// **Validates: Requirements 7.1, 7.2, 7.4**
// Property 13: Cache put/get round-trip
// For any tool name, input parameters, and result JSON, storing a value with Put
// and then retrieving it with Get using the same tool name and input parameters
// SHALL return a cache entry whose result field is byte-equal to the original,
// and whose metadata contains a valid timestamp, the tool name, and a non-empty input hash.
func TestProperty13_CachePutGetRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Setup temp cache directory
		tmpDir, err := os.MkdirTemp("", "cache-test-*")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)

		cache, err := NewResultCache(tmpDir)
		if err != nil {
			t.Fatal(err)
		}

		toolName := toolNameGen().Draw(t, "toolName")
		itemKey := itemKeyGen().Draw(t, "itemKey")
		inputParams := inputParamsGen().Draw(t, "inputParams")
		result := resultJSONGen().Draw(t, "result")

		before := time.Now().UTC().Add(-time.Second)

		// Put the result
		err = cache.Put(toolName, itemKey, inputParams, result)
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		after := time.Now().UTC().Add(time.Second)

		// Get with same parameters
		entry, err := cache.Get(toolName, itemKey, inputParams)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}

		// Entry must not be nil
		if entry == nil {
			t.Fatal("Get returned nil after Put with same parameters")
		}

		// Result must be byte-equal
		if string(entry.Result) != string(result) {
			t.Fatalf("Result mismatch: got %q, want %q", string(entry.Result), string(result))
		}

		// Metadata: tool name must match
		if entry.ToolName != toolName {
			t.Fatalf("ToolName mismatch: got %q, want %q", entry.ToolName, toolName)
		}

		// Metadata: item key must match
		if entry.ItemKey != itemKey {
			t.Fatalf("ItemKey mismatch: got %q, want %q", entry.ItemKey, itemKey)
		}

		// Metadata: input hash must be non-empty
		if entry.InputHash == "" {
			t.Fatal("InputHash is empty")
		}

		// Metadata: timestamp must be valid (between before and after)
		if entry.Timestamp.Before(before) || entry.Timestamp.After(after) {
			t.Fatalf("Timestamp %v not in expected range [%v, %v]", entry.Timestamp, before, after)
		}
	})
}

// **Validates: Requirements 7.5**
// Property 14: Selective cache invalidation
// For any two distinct tool names A and B with cached results, invalidating tool A
// SHALL cause Get(A) to return nil while Get(B) continues to return B's cached result unchanged.
func TestProperty14_SelectiveCacheInvalidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Setup temp cache directory
		tmpDir, err := os.MkdirTemp("", "cache-test-*")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)

		cache, err := NewResultCache(tmpDir)
		if err != nil {
			t.Fatal(err)
		}

		// Generate two distinct tool names
		toolNameA := toolNameGen().Draw(t, "toolNameA")
		toolNameB := toolNameGen().Draw(t, "toolNameB")
		// Ensure they are distinct
		if toolNameA == toolNameB {
			toolNameB = toolNameB + "x"
		}

		itemKeyA := itemKeyGen().Draw(t, "itemKeyA")
		itemKeyB := itemKeyGen().Draw(t, "itemKeyB")
		inputParamsA := inputParamsGen().Draw(t, "inputParamsA")
		inputParamsB := inputParamsGen().Draw(t, "inputParamsB")
		resultA := resultJSONGen().Draw(t, "resultA")
		resultB := resultJSONGen().Draw(t, "resultB")

		// Put both results
		err = cache.Put(toolNameA, itemKeyA, inputParamsA, resultA)
		if err != nil {
			t.Fatalf("Put A failed: %v", err)
		}
		err = cache.Put(toolNameB, itemKeyB, inputParamsB, resultB)
		if err != nil {
			t.Fatalf("Put B failed: %v", err)
		}

		// Verify both exist before invalidation
		entryA, err := cache.Get(toolNameA, itemKeyA, inputParamsA)
		if err != nil {
			t.Fatalf("Get A before invalidation failed: %v", err)
		}
		if entryA == nil {
			t.Fatal("Get A returned nil before invalidation")
		}

		entryB, err := cache.Get(toolNameB, itemKeyB, inputParamsB)
		if err != nil {
			t.Fatalf("Get B before invalidation failed: %v", err)
		}
		if entryB == nil {
			t.Fatal("Get B returned nil before invalidation")
		}

		// Invalidate tool A only
		err = cache.Invalidate(toolNameA)
		if err != nil {
			t.Fatalf("Invalidate A failed: %v", err)
		}

		// Get A should now return nil
		entryA, err = cache.Get(toolNameA, itemKeyA, inputParamsA)
		if err != nil {
			t.Fatalf("Get A after invalidation failed: %v", err)
		}
		if entryA != nil {
			t.Fatal("Get A should return nil after invalidation, but got a result")
		}

		// Get B should still return B's result unchanged
		entryB, err = cache.Get(toolNameB, itemKeyB, inputParamsB)
		if err != nil {
			t.Fatalf("Get B after invalidation of A failed: %v", err)
		}
		if entryB == nil {
			t.Fatal("Get B returned nil after invalidation of A — selective invalidation failed")
		}
		if string(entryB.Result) != string(resultB) {
			t.Fatalf("Result B changed after invalidation of A: got %q, want %q",
				string(entryB.Result), string(resultB))
		}
	})
}
