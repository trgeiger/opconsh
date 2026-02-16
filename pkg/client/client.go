package client

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	olmv1 "github.com/operator-framework/operator-controller/api/v1"
)

// OLMClient provides a client for interacting with OLM resources
type OLMClient struct {
	dynamic dynamic.Interface
}

// NewOLMClient creates a new OLM client
func NewOLMClient(config *rest.Config) (*OLMClient, error) {
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &OLMClient{
		dynamic: dynamicClient,
	}, nil
}

// ClusterCatalog operations

// GetClusterCatalogs retrieves all ClusterCatalogs
func (c *OLMClient) GetClusterCatalogs(ctx context.Context) ([]olmv1.ClusterCatalog, error) {
	gvr := schema.GroupVersionResource{
		Group:    "olm.operatorframework.io",
		Version:  "v1",
		Resource: "clustercatalogs",
	}

	list, err := c.dynamic.Resource(gvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var catalogs []olmv1.ClusterCatalog
	for _, item := range list.Items {
		var catalog olmv1.ClusterCatalog
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &catalog); err != nil {
			return nil, err
		}
		catalogs = append(catalogs, catalog)
	}

	return catalogs, nil
}

// GetClusterCatalog retrieves a specific ClusterCatalog by name
func (c *OLMClient) GetClusterCatalog(ctx context.Context, name string) (*olmv1.ClusterCatalog, error) {
	gvr := schema.GroupVersionResource{
		Group:    "olm.operatorframework.io",
		Version:  "v1",
		Resource: "clustercatalogs",
	}

	obj, err := c.dynamic.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	var catalog olmv1.ClusterCatalog
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &catalog); err != nil {
		return nil, err
	}

	return &catalog, nil
}

// ClusterExtension operations

// GetClusterExtensions retrieves all ClusterExtensions
func (c *OLMClient) GetClusterExtensions(ctx context.Context) ([]olmv1.ClusterExtension, error) {
	gvr := schema.GroupVersionResource{
		Group:    "olm.operatorframework.io",
		Version:  "v1",
		Resource: "clusterextensions",
	}

	list, err := c.dynamic.Resource(gvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var extensions []olmv1.ClusterExtension
	for _, item := range list.Items {
		var extension olmv1.ClusterExtension
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &extension); err != nil {
			return nil, err
		}
		extensions = append(extensions, extension)
	}

	return extensions, nil
}

// GetClusterExtension retrieves a specific ClusterExtension by name
func (c *OLMClient) GetClusterExtension(ctx context.Context, name string) (*olmv1.ClusterExtension, error) {
	gvr := schema.GroupVersionResource{
		Group:    "olm.operatorframework.io",
		Version:  "v1",
		Resource: "clusterextensions",
	}

	obj, err := c.dynamic.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	var extension olmv1.ClusterExtension
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &extension); err != nil {
		return nil, err
	}

	return &extension, nil
}
