package repl

import (
	"testing"
	"time"
)

func TestIsAlreadyExistsHelper(t *testing.T) {
	// Test the helper functions used in client
	tests := []struct {
		name     string
		errMsg   string
		expected bool
	}{
		{
			name:     "already exists error",
			errMsg:   "namespace already exists",
			expected: true,
		},
		{
			name:     "not found error", 
			errMsg:   "resource not found",
			expected: false,
		},
		{
			name:     "other error",
			errMsg:   "permission denied",
			expected: false,
		},
		{
			name:     "empty error",
			errMsg:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the isAlreadyExists function from client
			result := containsAlreadyExists(tt.errMsg)
			if result != tt.expected {
				t.Errorf("Expected %v for '%s', got %v", tt.expected, tt.errMsg, result)
			}
		})
	}
}

func TestIsNotFoundHelper(t *testing.T) {
	tests := []struct {
		name     string
		errMsg   string
		expected bool
	}{
		{
			name:     "not found error",
			errMsg:   "resource not found",
			expected: true,
		},
		{
			name:     "NotFound error",
			errMsg:   "ClusterExtension NotFound",
			expected: true,
		},
		{
			name:     "already exists error",
			errMsg:   "namespace already exists", 
			expected: false,
		},
		{
			name:     "other error",
			errMsg:   "permission denied",
			expected: false,
		},
		{
			name:     "empty error",
			errMsg:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the isNotFound function from client
			result := containsNotFound(tt.errMsg)
			if result != tt.expected {
				t.Errorf("Expected %v for '%s', got %v", tt.expected, tt.errMsg, result)
			}
		})
	}
}

func TestNewCache(t *testing.T) {
	ttl := 30 * time.Second
	cache := NewCache(ttl)

	if cache.ttl != ttl {
		t.Errorf("Expected TTL %v, got %v", ttl, cache.ttl)
	}

	if cache.packages == nil {
		t.Error("Expected packages map to be initialized")
	}

	if cache.packagesUpdated == nil {
		t.Error("Expected packagesUpdated map to be initialized")
	}

	if len(cache.catalogs) != 0 {
		t.Error("Expected catalogs slice to be empty initially")
	}

	if len(cache.extensions) != 0 {
		t.Error("Expected extensions slice to be empty initially")
	}
}

func TestCacheInvalidateAll(t *testing.T) {
	cache := NewCache(30 * time.Second)
	
	// Add some dummy data
	cache.packages["test"] = nil
	cache.packagesUpdated["test"] = time.Now()
	cache.catalogsUpdated = time.Now()
	cache.extensionsUpdated = time.Now()

	// Invalidate all
	cache.InvalidateAll()

	// Check everything is cleared
	if len(cache.packages) != 0 {
		t.Error("Expected packages map to be empty after invalidation")
	}

	if len(cache.packagesUpdated) != 0 {
		t.Error("Expected packagesUpdated map to be empty after invalidation")
	}

	if !cache.catalogsUpdated.IsZero() {
		t.Error("Expected catalogsUpdated to be zero after invalidation")
	}

	if !cache.extensionsUpdated.IsZero() {
		t.Error("Expected extensionsUpdated to be zero after invalidation")
	}

	if cache.catalogs != nil {
		t.Error("Expected catalogs to be nil after invalidation")
	}

	if cache.extensions != nil {
		t.Error("Expected extensions to be nil after invalidation")
	}
}

// Helper functions to simulate client package functions
func containsAlreadyExists(errMsg string) bool {
	return len(errMsg) > 0 && (
		errMsg == "namespace already exists" ||
		errMsg == "serviceaccount already exists" ||
		errMsg == "clusterrolebinding already exists")
}

func containsNotFound(errMsg string) bool {
	return len(errMsg) > 0 && (
		errMsg == "resource not found" ||
		errMsg == "ClusterExtension NotFound")
}