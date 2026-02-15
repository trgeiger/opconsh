package repl

import (
	"sync"
	"time"

	"github.com/operator-framework/opconsh/pkg/client"
	olmv1 "github.com/operator-framework/operator-controller/api/v1"
)

// Cache holds cached completion data to avoid API calls on every tab press
type Cache struct {
	mu sync.RWMutex
	
	// Cached data
	catalogs   []olmv1.ClusterCatalog
	extensions []olmv1.ClusterExtension
	packages   map[string][]client.Package // catalog name -> packages
	
	// Cache timestamps
	catalogsUpdated   time.Time
	extensionsUpdated time.Time
	packagesUpdated   map[string]time.Time // catalog name -> timestamp
	
	// Cache TTL
	ttl time.Duration
}

// NewCache creates a new cache with the specified TTL
func NewCache(ttl time.Duration) *Cache {
	return &Cache{
		packages:        make(map[string][]client.Package),
		packagesUpdated: make(map[string]time.Time),
		ttl:             ttl,
	}
}

// GetCatalogs returns cached catalogs or fetches them if cache is stale
func (c *Cache) GetCatalogs(r *REPL) ([]olmv1.ClusterCatalog, error) {
	c.mu.RLock()
	if time.Since(c.catalogsUpdated) < c.ttl && len(c.catalogs) > 0 {
		catalogs := c.catalogs
		c.mu.RUnlock()
		return catalogs, nil
	}
	c.mu.RUnlock()

	// Cache is stale, fetch fresh data
	catalogs, err := r.olmClient.GetClusterCatalogs(r.ctx)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.catalogs = catalogs
	c.catalogsUpdated = time.Now()
	c.mu.Unlock()

	return catalogs, nil
}

// GetExtensions returns cached extensions or fetches them if cache is stale
func (c *Cache) GetExtensions(r *REPL) ([]olmv1.ClusterExtension, error) {
	c.mu.RLock()
	if time.Since(c.extensionsUpdated) < c.ttl && len(c.extensions) > 0 {
		extensions := c.extensions
		c.mu.RUnlock()
		return extensions, nil
	}
	c.mu.RUnlock()

	// Cache is stale, fetch fresh data
	extensions, err := r.olmClient.GetClusterExtensions(r.ctx)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.extensions = extensions
	c.extensionsUpdated = time.Now()
	c.mu.Unlock()

	return extensions, nil
}

// GetPackages returns cached packages for a catalog or fetches them if cache is stale
func (c *Cache) GetPackages(r *REPL, catalogName, catalogBaseURL string) ([]client.Package, error) {
	c.mu.RLock()
	if lastUpdated, exists := c.packagesUpdated[catalogName]; exists {
		if time.Since(lastUpdated) < c.ttl {
			packages := c.packages[catalogName]
			c.mu.RUnlock()
			return packages, nil
		}
	}
	c.mu.RUnlock()

	// Cache is stale, fetch fresh data
	packages, err := r.catalogdClient.GetPackages(r.ctx, catalogName, catalogBaseURL)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.packages[catalogName] = packages
	c.packagesUpdated[catalogName] = time.Now()
	c.mu.Unlock()

	return packages, nil
}

// InvalidateAll clears all cached data
func (c *Cache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.catalogs = nil
	c.extensions = nil
	c.packages = make(map[string][]client.Package)
	c.catalogsUpdated = time.Time{}
	c.extensionsUpdated = time.Time{}
	c.packagesUpdated = make(map[string]time.Time)
}