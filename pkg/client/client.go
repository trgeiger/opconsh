package client

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	olmv1 "github.com/operator-framework/operator-controller/api/v1"
)

// OLMClient provides a client for interacting with OLM resources
type OLMClient struct {
	dynamic   dynamic.Interface
	clientset kubernetes.Interface
}

// NewOLMClient creates a new OLM client
func NewOLMClient(config *rest.Config) (*OLMClient, error) {
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &OLMClient{
		dynamic:   dynamicClient,
		clientset: clientset,
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

// CreateNamespace creates a namespace if it doesn't exist
func (c *OLMClient) CreateNamespace(ctx context.Context, name string) error {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"created-by": "opconsh",
				"purpose":    "experimental-extension-install",
			},
		},
	}

	_, err := c.clientset.CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{})
	if err != nil && !isAlreadyExists(err) {
		return err
	}
	return nil
}

// CreateServiceAccount creates a ServiceAccount for experimental extension installation
func (c *OLMClient) CreateServiceAccount(ctx context.Context, namespace, name string) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"created-by": "opconsh",
				"purpose":    "experimental-extension-install",
			},
		},
	}

	_, err := c.clientset.CoreV1().ServiceAccounts(namespace).Create(ctx, sa, metav1.CreateOptions{})
	if err != nil && !isAlreadyExists(err) {
		return err
	}
	return nil
}

// CreateClusterRoleBinding creates a ClusterRoleBinding with cluster-admin privileges
func (c *OLMClient) CreateClusterRoleBinding(ctx context.Context, name, serviceAccountName, serviceAccountNamespace string) error {
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"created-by": "opconsh",
				"purpose":    "experimental-extension-install",
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "cluster-admin",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      serviceAccountName,
				Namespace: serviceAccountNamespace,
			},
		},
	}

	_, err := c.clientset.RbacV1().ClusterRoleBindings().Create(ctx, crb, metav1.CreateOptions{})
	if err != nil && !isAlreadyExists(err) {
		return err
	}
	return nil
}

// CreateClusterExtension creates a ClusterExtension
func (c *OLMClient) CreateClusterExtension(ctx context.Context, extension *olmv1.ClusterExtension) error {
	gvr := schema.GroupVersionResource{
		Group:    "olm.operatorframework.io",
		Version:  "v1",
		Resource: "clusterextensions",
	}

	// Convert to unstructured
	unstructuredData, err := runtime.DefaultUnstructuredConverter.ToUnstructured(extension)
	if err != nil {
		return err
	}

	unstructuredObj := &unstructured.Unstructured{Object: unstructuredData}
	_, err = c.dynamic.Resource(gvr).Create(ctx, unstructuredObj, metav1.CreateOptions{})
	return err
}

// DeleteClusterExtension deletes a ClusterExtension
func (c *OLMClient) DeleteClusterExtension(ctx context.Context, name string) error {
	gvr := schema.GroupVersionResource{
		Group:    "olm.operatorframework.io",
		Version:  "v1",
		Resource: "clusterextensions",
	}

	err := c.dynamic.Resource(gvr).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !isNotFound(err) {
		return err
	}
	return nil
}

// DeleteClusterRoleBinding deletes a ClusterRoleBinding if it exists and was created by opconsh
func (c *OLMClient) DeleteClusterRoleBinding(ctx context.Context, name string) error {
	// First, check if it exists and has our labels
	crb, err := c.clientset.RbacV1().ClusterRoleBindings().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if isNotFound(err) {
			return nil // Already gone
		}
		return err
	}

	// Only delete if it has our labels (safety check)
	if crb.Labels["created-by"] != "opconsh" || crb.Labels["purpose"] != "experimental-extension-install" {
		return fmt.Errorf("ClusterRoleBinding '%s' was not created by opconsh, refusing to delete", name)
	}

	err = c.clientset.RbacV1().ClusterRoleBindings().Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !isNotFound(err) {
		return err
	}
	return nil
}

// DeleteServiceAccount deletes a ServiceAccount if it exists and was created by opconsh
func (c *OLMClient) DeleteServiceAccount(ctx context.Context, namespace, name string) error {
	// First, check if it exists and has our labels
	sa, err := c.clientset.CoreV1().ServiceAccounts(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if isNotFound(err) {
			return nil // Already gone
		}
		return err
	}

	// Only delete if it has our labels (safety check)
	if sa.Labels["created-by"] != "opconsh" || sa.Labels["purpose"] != "experimental-extension-install" {
		return fmt.Errorf("ServiceAccount '%s' was not created by opconsh, refusing to delete", name)
	}

	err = c.clientset.CoreV1().ServiceAccounts(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !isNotFound(err) {
		return err
	}
	return nil
}

// DeleteNamespaceIfEmpty deletes a namespace if it exists, was created by opconsh, and is empty of user resources
func (c *OLMClient) DeleteNamespaceIfEmpty(ctx context.Context, name string) error {
	// First, check if it exists and has our labels
	ns, err := c.clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if isNotFound(err) {
			return nil // Already gone
		}
		return err
	}

	// Only delete if it has our labels (safety check)
	if ns.Labels["created-by"] != "opconsh" || ns.Labels["purpose"] != "experimental-extension-install" {
		return fmt.Errorf("namespace '%s' was not created by opconsh, refusing to delete", name)
	}

	// Check if namespace has any user-created resources (excluding system resources)
	// For now, just check if it's empty of pods, services, deployments, etc.
	pods, err := c.clientset.CoreV1().Pods(name).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if len(pods.Items) > 0 {
		return nil // Don't delete namespace with pods
	}

	// Check for other common resources
	services, err := c.clientset.CoreV1().Services(name).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if len(services.Items) > 0 {
		return nil // Don't delete namespace with services
	}

	err = c.clientset.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !isNotFound(err) {
		return err
	}
	return nil
}

// Helper function to check if error is "already exists"
func isAlreadyExists(err error) bool {
	return strings.Contains(err.Error(), "already exists")
}

// Helper function to check if error is "not found"
func isNotFound(err error) bool {
	return strings.Contains(err.Error(), "not found")
}
