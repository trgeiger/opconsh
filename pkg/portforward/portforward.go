package portforward

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// PortForwarder manages port forwarding to catalogd service
type PortForwarder struct {
	config    *rest.Config
	clientset *kubernetes.Clientset
	forwarder *portforward.PortForwarder
	stopCh    chan struct{}
	readyCh   chan struct{}
	localPort string
	ctx       context.Context
}

// NewPortForwarder creates a new port forwarder
func NewPortForwarder(config *rest.Config, clientset *kubernetes.Clientset) *PortForwarder {
	return &PortForwarder{
		config:    config,
		clientset: clientset,
		localPort: "8080",
		ctx:       context.Background(),
	}
}

// Start begins port forwarding to catalogd service
func (pf *PortForwarder) Start() error {
	// First, find the catalogd namespace and pod
	catalogdNamespace, err := pf.findCatalogdNamespace()
	if err != nil {
		return fmt.Errorf("failed to find catalogd namespace: %w", err)
	}

	pods, err := pf.clientset.CoreV1().Pods(catalogdNamespace).List(pf.ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=catalogd",
	})
	if err != nil {
		return fmt.Errorf("failed to list catalogd pods: %w", err)
	}

	if len(pods.Items) == 0 {
		return fmt.Errorf("no catalogd pods found in namespace %s", catalogdNamespace)
	}

	podName := pods.Items[0].Name

	transport, upgrader, err := spdy.RoundTripperFor(pf.config)
	if err != nil {
		return fmt.Errorf("failed to create round tripper: %w", err)
	}

	serverURL, err := url.Parse(pf.config.Host)
	if err != nil {
		return fmt.Errorf("failed to parse server URL: %w", err)
	}

	// Build port forward URL to the catalogd pod
	portForwardURL := *serverURL
	portForwardURL.Path = fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", catalogdNamespace, podName)

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, &portForwardURL)

	pf.stopCh = make(chan struct{}, 1)
	pf.readyCh = make(chan struct{})
	// Map local port 8080 to pod port 8443 (catalogd container port)
	ports := []string{fmt.Sprintf("%s:8443", pf.localPort)}

	// Create port forwarder
	pf.forwarder, err = portforward.New(dialer, ports, pf.stopCh, pf.readyCh, io.Discard, io.Discard)
	if err != nil {
		return fmt.Errorf("failed to create port forwarder: %w", err)
	}

	// Start port forwarding in background
	go func() {
		if err := pf.forwarder.ForwardPorts(); err != nil {
			fmt.Printf("Port forwarding error: %v\n", err)
		}
	}()

	// Wait for port forward to be ready
	select {
	case <-pf.readyCh:
		return nil
	case <-time.After(10 * time.Second):
		pf.Stop()
		return fmt.Errorf("timeout waiting for port forward to be ready")
	}
}

// Stop stops the port forwarding
func (pf *PortForwarder) Stop() {
	if pf.stopCh != nil {
		close(pf.stopCh)
	}
}

// GetLocalURL returns the local URL for accessing catalogd
func (pf *PortForwarder) GetLocalURL() string {
	return fmt.Sprintf("https://localhost:%s", pf.localPort)
}

// IsReady checks if the port forward is ready
func (pf *PortForwarder) IsReady() bool {
	if pf.forwarder == nil {
		return false
	}
	// Check if forwarder is still running and ready channel was signaled
	select {
	case <-pf.readyCh:
		return true
	default:
		return false
	}
}

// findCatalogdNamespace searches for the namespace containing catalogd deployment
func (pf *PortForwarder) findCatalogdNamespace() (string, error) {
	// Common catalogd namespaces to check, in order of preference
	candidateNamespaces := []string{
		"openshift-catalogd",         // OpenShift default
		"olmv1-system",               // Upstream default
		"operator-lifecycle-manager", // Alternative
	}

	// First, try the common namespaces
	for _, ns := range candidateNamespaces {
		pods, err := pf.clientset.CoreV1().Pods(ns).List(pf.ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/name=catalogd",
		})
		if err != nil {
			// Namespace might not exist, continue checking
			continue
		}

		if len(pods.Items) > 0 {
			return ns, nil
		}
	}

	// If not found in common namespaces, search all namespaces
	namespaces, err := pf.clientset.CoreV1().Namespaces().List(pf.ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to list namespaces: %w", err)
	}

	for _, ns := range namespaces.Items {
		// Skip namespaces we already checked
		skip := false
		for _, candidate := range candidateNamespaces {
			if ns.Name == candidate {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		pods, err := pf.clientset.CoreV1().Pods(ns.Name).List(pf.ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/name=catalogd",
		})
		if err != nil {
			continue
		}

		if len(pods.Items) > 0 {
			return ns.Name, nil
		}
	}

	return "", fmt.Errorf("no catalogd pods found in any namespace")
}
