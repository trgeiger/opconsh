package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"k8s.io/client-go/rest"
)

// CatalogdClient provides a client for querying catalogd APIs
type CatalogdClient struct {
	httpClient *http.Client
	baseURL    string
}

// Package represents a package in a catalog
type Package struct {
	Name           string    `json:"name"`
	DefaultChannel string    `json:"defaultChannel"`
	Channels       []Channel `json:"channels"`
	Icon           *Icon     `json:"icon,omitempty"` // Single icon, not array
}

// Channel represents a channel in a package
type Channel struct {
	Name    string   `json:"name"`
	Entries []Bundle `json:"entries"`
}

// Bundle represents a bundle in a channel
type Bundle struct {
	Name          string            `json:"name"`
	Version       string            `json:"version"`
	Image         string            `json:"image"`
	Description   string            `json:"description"`
	DisplayName   string            `json:"displayName,omitempty"`
	Properties    []json.RawMessage `json:"properties,omitempty"`
	RelatedImages []RelatedImage    `json:"relatedImages,omitempty"`
}

// RelatedImage represents an image referenced by a bundle
type RelatedImage struct {
	Name  string `json:"name"`
	Image string `json:"image"`
}

// Icon represents an icon for a package
type Icon struct {
	Base64data string `json:"base64data"`
	MediaType  string `json:"mediatype"`
}

// NewCatalogdClient creates a new catalogd client
func NewCatalogdClient(config *rest.Config) (*CatalogdClient, error) {
	// Create HTTP client with TLS verification disabled for localhost connections
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // Skip TLS verification for localhost port-forward
		},
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	return &CatalogdClient{
		httpClient: httpClient,
		baseURL:    "", // Will get the URL from ClusterCatalog status
	}, nil
}

// GetPackages returns all packages from a specific catalog using the ClusterCatalog status URL
func (c *CatalogdClient) GetPackages(ctx context.Context, catalogName string, catalogBaseURL string) ([]Package, error) {
	// Transform the cluster-internal URL to localhost if we're running locally
	finalURL, err := c.transformURLForLocal(catalogBaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to transform URL: %w", err)
	}

	// Use the transformed URL and append the API path
	apiURL := fmt.Sprintf("%s/api/v1/all", finalURL)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request to %s: %w\n\nHint: Make sure you have port-forwarded the catalogd service:\n  kubectl port-forward svc/catalogd-service 8080:443 -n <catalogd-namespace>\n  (catalogd-namespace is typically 'olmv1-system' or 'openshift-catalogd')", apiURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("catalogd API returned status %d: %s", resp.StatusCode, string(body))
	}

	// The catalogd API returns a stream of JSON objects, each representing different content types
	// We need to parse them and extract packages, channels, and bundles to build complete package info
	var packages []Package
	packageMap := make(map[string]*Package)
	channelMap := make(map[string][]Channel) // package name -> channels
	bundleMap := make(map[string][]Bundle)   // package name -> bundles
	decoder := json.NewDecoder(resp.Body)

	// First pass: collect all packages, channels, and bundles
	for decoder.More() {
		var item map[string]interface{}
		if err := decoder.Decode(&item); err != nil {
			return nil, fmt.Errorf("failed to decode response item: %w", err)
		}

		schema, ok := item["schema"].(string)
		if !ok {
			continue
		}

		switch schema {
		case "olm.package":
			// Convert back to JSON and then to Package struct
			jsonBytes, err := json.Marshal(item)
			if err != nil {
				continue
			}

			var pkg Package
			if err := json.Unmarshal(jsonBytes, &pkg); err != nil {
				continue
			}

			packageMap[pkg.Name] = &pkg

		case "olm.channel":
			if pkgName, ok := item["package"].(string); ok {
				jsonBytes, err := json.Marshal(item)
				if err != nil {
					continue
				}

				var channel Channel
				if err := json.Unmarshal(jsonBytes, &channel); err != nil {
					continue
				}

				channelMap[pkgName] = append(channelMap[pkgName], channel)
			}

		case "olm.bundle":
			if pkgName, ok := item["package"].(string); ok {
				jsonBytes, err := json.Marshal(item)
				if err != nil {
					continue
				}

				var bundle Bundle
				if err := json.Unmarshal(jsonBytes, &bundle); err != nil {
					continue
				}

				// Extract description, display name, and version from properties
				bundle.Description, bundle.DisplayName, bundle.Version = extractFromProperties(bundle.Properties)

				bundleMap[pkgName] = append(bundleMap[pkgName], bundle)
			}
		}
	}

	// Second pass: associate bundles with channels based on channel entries
	for pkgName, channels := range channelMap {
		pkg, exists := packageMap[pkgName]
		if !exists {
			continue
		}

		// For each channel, populate its entries with matching bundles
		for i, channel := range channels {
			var channelBundles []Bundle

			// Channels define which bundles they contain via "entries" array with bundle names
			for _, entry := range channel.Entries {
				// Find the matching bundle from our bundle map
				if pkgBundles, hasPackage := bundleMap[pkgName]; hasPackage {
					for _, bundle := range pkgBundles {
						if bundle.Name == entry.Name {
							channelBundles = append(channelBundles, bundle)
							break
						}
					}
				}
			}

			channels[i].Entries = channelBundles
		}

		pkg.Channels = channels
	}

	// Convert map to slice and sort alphabetically
	for _, pkg := range packageMap {
		packages = append(packages, *pkg)
	}

	// Sort packages alphabetically by name
	sort.Slice(packages, func(i, j int) bool {
		return packages[i].Name < packages[j].Name
	})

	return packages, nil
}

// GetPackage returns a specific package from a catalog
func (c *CatalogdClient) GetPackage(ctx context.Context, catalogName, packageName, catalogBaseURL string) (*Package, error) {
	packages, err := c.GetPackages(ctx, catalogName, catalogBaseURL)
	if err != nil {
		return nil, err
	}

	for _, pkg := range packages {
		if pkg.Name == packageName {
			return &pkg, nil
		}
	}

	return nil, fmt.Errorf("package %s not found in catalog %s", packageName, catalogName)
}

// SearchPackages searches for packages by name across a catalog
func (c *CatalogdClient) SearchPackages(ctx context.Context, catalogName, searchTerm, catalogBaseURL string) ([]Package, error) {
	packages, err := c.GetPackages(ctx, catalogName, catalogBaseURL)
	if err != nil {
		return nil, err
	}

	var matches []Package
	searchLower := strings.ToLower(searchTerm)

	for _, pkg := range packages {
		if strings.Contains(strings.ToLower(pkg.Name), searchLower) {
			matches = append(matches, pkg)
		}
	}

	return matches, nil
}

// GetBundles returns all bundles for a specific package and channel
func (c *CatalogdClient) GetBundles(ctx context.Context, catalogName, packageName, channelName, catalogBaseURL string) ([]Bundle, error) {
	pkg, err := c.GetPackage(ctx, catalogName, packageName, catalogBaseURL)
	if err != nil {
		return nil, err
	}

	for _, channel := range pkg.Channels {
		if channel.Name == channelName {
			return channel.Entries, nil
		}
	}

	return nil, fmt.Errorf("channel %s not found for package %s", channelName, packageName)
}

// transformURLForLocal transforms cluster-internal service URLs to localhost URLs
// for when the tool is running locally with port-forwarding
func (c *CatalogdClient) transformURLForLocal(clusterURL string) (string, error) {
	parsedURL, err := url.Parse(clusterURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL %s: %w", clusterURL, err)
	}

	// Check if this is a cluster-internal service URL
	if strings.Contains(parsedURL.Host, "catalogd-service") && strings.Contains(parsedURL.Host, ".svc") {
		// Transform to localhost with port 8080 (assuming user has port-forwarded)
		// From: https://catalogd-service.olmv1-system.svc/catalogs/operatorhubio
		// To:   https://localhost:8080/catalogs/operatorhubio
		parsedURL.Host = "localhost:8080"
		return parsedURL.String(), nil
	}

	// If it's already a localhost URL or external URL, return as-is
	return clusterURL, nil
}

// extractFromProperties extracts description, displayName, and version from bundle properties
func extractFromProperties(properties []json.RawMessage) (string, string, string) {
	var description, displayName, version string

	for _, prop := range properties {
		var property map[string]interface{}
		if err := json.Unmarshal(prop, &property); err != nil {
			continue
		}

		propType, ok := property["type"].(string)
		if !ok {
			continue
		}

		if propType == "olm.csv.metadata" {
			value, ok := property["value"].(map[string]interface{})
			if !ok {
				continue
			}

			if desc, ok := value["description"].(string); ok {
				description = desc
			}

			if name, ok := value["displayName"].(string); ok {
				displayName = name
			}
		}

		if propType == "olm.package" {
			value, ok := property["value"].(map[string]interface{})
			if !ok {
				continue
			}

			if ver, ok := value["version"].(string); ok {
				version = ver
			}
		}
	}

	return description, displayName, version
}

// FindBestDescription searches through all bundles in all channels to find the best description
func FindBestDescription(pkg *Package) (string, string) {
	var bestDescription, bestDisplayName string
	bestDescLength := 0

	// Helper function to check a bundle and update best if it's better
	checkBundle := func(bundle Bundle) {
		if len(bundle.Description) > bestDescLength {
			bestDescription = bundle.Description
			bestDisplayName = bundle.DisplayName
			bestDescLength = len(bundle.Description)
		}
	}

	// First priority: check default channel bundles
	for _, channel := range pkg.Channels {
		if channel.Name == pkg.DefaultChannel {
			for _, bundle := range channel.Entries {
				checkBundle(bundle)
			}
			// If we found a good description in default channel, prefer it
			if bestDescLength > 100 { // Decent length description
				return bestDescription, bestDisplayName
			}
			break
		}
	}

	// Second priority: check all other channels if we don't have a good description
	if bestDescLength < 100 {
		for _, channel := range pkg.Channels {
			if channel.Name != pkg.DefaultChannel {
				for _, bundle := range channel.Entries {
					checkBundle(bundle)
				}
			}
		}
	}

	// Return best found, or fallback message
	if bestDescription == "" {
		return "No description available", ""
	}
	return bestDescription, bestDisplayName
}
