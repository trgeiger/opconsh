package portforward

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	// First, find the catalogd pod
	pods, err := pf.clientset.CoreV1().Pods("olmv1-system").List(pf.ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=catalogd",
	})
	if err != nil {
		return fmt.Errorf("failed to list catalogd pods: %w", err)
	}
	
	if len(pods.Items) == 0 {
		return fmt.Errorf("no catalogd pods found in olmv1-system namespace")
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
	portForwardURL.Path = fmt.Sprintf("/api/v1/namespaces/olmv1-system/pods/%s/portforward", podName)

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