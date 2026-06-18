package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNewResultCache_CreatesDirectory(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "cache-test-create-dir")
	defer os.RemoveAll(tmpDir)

	_, err := NewResultCache(tmpDir)
	if err != nil {
		t.Fatalf("NewResultCache failed: %v", err)
	}

	info, err := os.Stat(tmpDir)
	if err != nil {
		t.Fatalf("cache directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("cache path is not a directory")
	}
}

func TestGet_MissOnNonexistent(t *testing.T) {
	tmpDir := t.TempDir()
	cache, err := NewResultCache(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	entry, err := cache.Get("nonexistent", "key", map[string]string{"a": "b"})
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if entry != nil {
		t.Fatal("expected nil entry for cache miss")
	}
}

func TestGet_CorruptCacheDeletesAndReturnsMiss(t *testing.T) {
	tmpDir := t.TempDir()
	cache, err := NewResultCache(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Write corrupt data to the cache file
	toolDir := filepath.Join(tmpDir, "mytool")
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	corruptPath := filepath.Join(toolDir, "mykey.json")
	if err := os.WriteFile(corruptPath, []byte("not valid json{{{"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Get should return nil (miss) and delete the corrupt file
	entry, err := cache.Get("mytool", "mykey", map[string]string{})
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if entry != nil {
		t.Fatal("expected nil for corrupt cache")
	}

	// Verify file was deleted
	if _, err := os.Stat(corruptPath); !os.IsNotExist(err) {
		t.Fatal("corrupt cache file was not deleted")
	}
}

func TestGet_MismatchedInputHash(t *testing.T) {
	tmpDir := t.TempDir()
	cache, err := NewResultCache(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	result := json.RawMessage(`{"status":"ok"}`)
	paramsA := map[string]string{"key": "value_a"}
	paramsB := map[string]string{"key": "value_b"}

	// Put with paramsA
	if err := cache.Put("tool", "item", paramsA, result); err != nil {
		t.Fatal(err)
	}

	// Get with paramsB should miss
	entry, err := cache.Get("tool", "item", paramsB)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if entry != nil {
		t.Fatal("expected nil when input params don't match")
	}
}

func TestInvalidateItem(t *testing.T) {
	tmpDir := t.TempDir()
	cache, err := NewResultCache(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	result := json.RawMessage(`{"data":1}`)
	params := map[string]string{"x": "y"}

	// Put two items for same tool
	if err := cache.Put("tool", "item1", params, result); err != nil {
		t.Fatal(err)
	}
	if err := cache.Put("tool", "item2", params, result); err != nil {
		t.Fatal(err)
	}

	// Invalidate only item1
	if err := cache.InvalidateItem("tool", "item1"); err != nil {
		t.Fatal(err)
	}

	// item1 should be gone
	entry, err := cache.Get("tool", "item1", params)
	if err != nil {
		t.Fatal(err)
	}
	if entry != nil {
		t.Fatal("item1 should be invalidated")
	}

	// item2 should still exist
	entry, err = cache.Get("tool", "item2", params)
	if err != nil {
		t.Fatal(err)
	}
	if entry == nil {
		t.Fatal("item2 should still exist")
	}
}

func TestInvalidateAll(t *testing.T) {
	tmpDir := t.TempDir()
	cache, err := NewResultCache(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	result := json.RawMessage(`{"ok":true}`)
	params := map[string]string{}

	if err := cache.Put("tool_a", "key1", params, result); err != nil {
		t.Fatal(err)
	}
	if err := cache.Put("tool_b", "key2", params, result); err != nil {
		t.Fatal(err)
	}

	if err := cache.InvalidateAll(); err != nil {
		t.Fatal(err)
	}

	entry, _ := cache.Get("tool_a", "key1", params)
	if entry != nil {
		t.Fatal("tool_a should be gone after InvalidateAll")
	}

	entry, _ = cache.Get("tool_b", "key2", params)
	if entry != nil {
		t.Fatal("tool_b should be gone after InvalidateAll")
	}
}
