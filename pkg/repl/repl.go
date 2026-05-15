package repl

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/chzyer/readline"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/operator-framework/opconsh/pkg/client"
	"github.com/operator-framework/opconsh/pkg/portforward"
	olmv1 "github.com/operator-framework/operator-controller/api/v1"
)

// REPL represents the interactive Read-Eval-Print-Loop
type REPL struct {
	k8sClient      *kubernetes.Clientset
	olmClient      *client.OLMClient
	catalogdClient *client.CatalogdClient
	config         *rest.Config
	kubeConfig     clientcmd.ClientConfig
	ctx            context.Context
	cache          *Cache
	portForward    *portforward.PortForwarder // For non-interactive package commands
}

// New creates a new REPL instance
func New(k8sClient *kubernetes.Clientset, olmClient *client.OLMClient, catalogdClient *client.CatalogdClient, config *rest.Config, kubeConfig clientcmd.ClientConfig) *REPL {
	return &REPL{
		k8sClient:      k8sClient,
		olmClient:      olmClient,
		catalogdClient: catalogdClient,
		config:         config,
		kubeConfig:     kubeConfig,
		ctx:            context.Background(),
		cache:          NewCache(30 * time.Second), // 30 second cache TTL
	}
}

// Start begins the interactive REPL session
func (r *REPL) Start() error {
	fmt.Println("Welcome to opconsh - Interactive OLMv1 CLI")
	fmt.Println("Type 'help' for available commands or 'exit' to quit.")
	fmt.Println("Use Tab for command completion.")
	fmt.Println()

	// Set up readline with tab completion and proper history support
	completer := r.setupCompletion()
	rl, err := readline.NewEx(&readline.Config{
		Prompt:                 "opconsh> ",
		AutoComplete:           completer,
		HistoryFile:            "/tmp/.opconsh_history",
		HistoryLimit:           1000,
		DisableAutoSaveHistory: false,
		VimMode:                false, // Ensure we're in emacs mode for standard arrow key support
	})
	if err != nil {
		return fmt.Errorf("failed to create readline: %w", err)
	}
	defer rl.Close()

	for {
		input, err := rl.Readline()
		if err != nil {
			if err == io.EOF || err == readline.ErrInterrupt {
				fmt.Println("Goodbye!")
				return nil
			}
			return err
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		if input == "exit" || input == "quit" {
			fmt.Println("Goodbye!")
			return nil
		}

		if err := r.processCommand(input); err != nil {
			fmt.Printf("Error: %v\n", err)
		}
	}
}

// ProcessCommand handles the execution of commands (exported for non-interactive use)
func (r *REPL) ProcessCommand(input string) error {
	return r.processCommand(input)
}

// processCommand handles the execution of commands
func (r *REPL) processCommand(input string) error {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return nil
	}

	command := parts[0]
	args := parts[1:]

	switch command {
	case "help", "h":
		return r.showHelp()
	case "catalogs", "cc":
		return r.handleCatalogCommands(args)
	case "extensions", "ext":
		return r.handleExtensionCommands(args)
	case "status":
		return r.showStatus()
	case "refresh":
		return r.refreshCache()
	case "clear":
		return r.clearScreen()
	case "enter":
		if len(args) < 1 {
			return fmt.Errorf("'enter' requires a catalog name")
		}
		return r.EnterCatalogContext(args[0])
	case "diagnose":
		return r.handleDiagnoseCommands(args)
	default:
		return fmt.Errorf("unknown command: %s. Type 'help' for available commands", command)
	}
}

// showHelp displays available commands
func (r *REPL) showHelp() error {
	fmt.Println("Available commands:")
	fmt.Println()
	fmt.Println("  help, h                    Show this help message")
	fmt.Println("  catalogs, cc               Work with ClusterCatalogs")
	fmt.Println("    list                     List all ClusterCatalogs")
	fmt.Println("    get <name>              Get specific ClusterCatalog details")
	fmt.Println("    packages <catalog>      List packages in a catalog (legacy)")
	fmt.Println("    package <catalog> <pkg> Get detailed package information (legacy)")
	fmt.Println("    search <catalog> <term> Search packages in a catalog (legacy)")
	fmt.Println()
	fmt.Println("  enter <catalog>            Enter interactive catalog context")
	fmt.Println()
	fmt.Println("  extensions, ext            Work with ClusterExtensions")
	fmt.Println("    list                     List all ClusterExtensions")
	fmt.Println("    get <name>              Get specific ClusterExtension details")
	fmt.Println("    install-experimental     [⚠️ TESTING ONLY] Install extension with cluster-admin SA")
	fmt.Println("    uninstall-experimental   [⚠️ TESTING ONLY] Uninstall extension and cleanup RBAC")
	fmt.Println()
	fmt.Println("  diagnose                   Troubleshoot OLM resources")
	fmt.Println("    catalog <name>          Diagnose ClusterCatalog issues")
	fmt.Println("    extension <name>        Diagnose ClusterExtension issues")
	fmt.Println()
	fmt.Println("  status                     Show detailed cluster connection and OLM status")
	fmt.Println("  refresh                    Refresh cached completion data")
	fmt.Println("  clear                      Clear the screen")
	fmt.Println("  exit, quit                 Exit opconsh")
	fmt.Println()
	return nil
}

// showStatus displays detailed cluster connection and OLM status
func (r *REPL) showStatus() error {
	fmt.Println("Cluster Connection Status:")

	// Get kubeconfig context information
	rawConfig, err := r.kubeConfig.RawConfig()
	if err != nil {
		fmt.Printf("  [!] Kubeconfig: Unable to read config (%v)\n", err)
	} else {
		currentContext := rawConfig.CurrentContext
		if currentContext == "" {
			currentContext = "default"
		}

		// Get kubeconfig file path
		configAccess := r.kubeConfig.ConfigAccess()
		kubeconfigPath := "in-cluster"
		if configAccess != nil {
			if paths := configAccess.GetLoadingPrecedence(); len(paths) > 0 {
				kubeconfigPath = paths[0]
			}
		}

		fmt.Printf("  [+] Kubeconfig: %s (context: %s)\n", kubeconfigPath, currentContext)

		// Show current namespace if set
		namespace, _, err := r.kubeConfig.Namespace()
		if err == nil && namespace != "" {
			fmt.Printf("  [+] Namespace: %s\n", namespace)
		}
	}

	// Check API server connectivity and version
	version, err := r.k8sClient.Discovery().ServerVersion()
	if err != nil {
		fmt.Printf("  [!] API Server: Failed to connect (%v)\n", err)
		return err
	}

	fmt.Printf("  [+] API Server: %s (Kubernetes %s)\n", r.config.Host, version.GitVersion)

	// Test user permissions by trying to list namespaces
	namespaces, err := r.k8sClient.CoreV1().Namespaces().List(r.ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		fmt.Printf("  [?] Permissions: Limited access (%v)\n", err)
	} else {
		fmt.Printf("  [+] Permissions: Can list cluster resources (%d namespaces)\n", len(namespaces.Items))
	}

	fmt.Println()
	fmt.Println("OLM Status:")

	// Check catalogd availability
	catalogdNamespace, err := r.findCatalogdNamespace()
	if err != nil {
		fmt.Printf("  [!] Catalogd: Not found (%v)\n", err)
	} else {
		// Check catalogd pods
		pods, err := r.k8sClient.CoreV1().Pods(catalogdNamespace).List(r.ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/name=catalogd",
		})
		if err != nil {
			fmt.Printf("  [!] Catalogd: Error checking pods in %s (%v)\n", catalogdNamespace, err)
		} else if len(pods.Items) == 0 {
			fmt.Printf("  [!] Catalogd: No pods found in %s\n", catalogdNamespace)
		} else {
			readyPods := 0
			for _, pod := range pods.Items {
				for _, condition := range pod.Status.Conditions {
					if condition.Type == "Ready" && condition.Status == "True" {
						readyPods++
						break
					}
				}
			}
			if readyPods == 0 {
				fmt.Printf("  [?] Catalogd: %d pod(s) in %s, none ready\n", len(pods.Items), catalogdNamespace)
			} else {
				fmt.Printf("  [+] Catalogd: %d/%d pod(s) ready in %s\n", readyPods, len(pods.Items), catalogdNamespace)
			}
		}
	}

	// Check operator-controller availability
	operatorControllerNamespace, err := r.findOperatorControllerNamespace()
	if err != nil {
		fmt.Printf("  [!] Operator Controller: Not found (%v)\n", err)
	} else {
		// Check operator-controller pods
		pods, err := r.k8sClient.CoreV1().Pods(operatorControllerNamespace).List(r.ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/name=operator-controller",
		})
		if err != nil {
			fmt.Printf("  [!] Operator Controller: Error checking pods in %s (%v)\n", operatorControllerNamespace, err)
		} else if len(pods.Items) == 0 {
			fmt.Printf("  [!] Operator Controller: No pods found in %s\n", operatorControllerNamespace)
		} else {
			readyPods := 0
			for _, pod := range pods.Items {
				for _, condition := range pod.Status.Conditions {
					if condition.Type == "Ready" && condition.Status == "True" {
						readyPods++
						break
					}
				}
			}
			if readyPods == 0 {
				fmt.Printf("  [?] Operator Controller: %d pod(s) in %s, none ready\n", len(pods.Items), operatorControllerNamespace)
			} else {
				fmt.Printf("  [+] Operator Controller: %d/%d pod(s) ready in %s\n", readyPods, len(pods.Items), operatorControllerNamespace)
			}
		}
	}

	// Check ClusterCatalogs with detailed error reporting
	catalogs, err := r.olmClient.GetClusterCatalogs(r.ctx)
	if err != nil {
		fmt.Printf("  [!] ClusterCatalogs: Unable to access (%v)\n", err)
	} else {
		availableCount := 0
		errorCount := 0
		for _, catalog := range catalogs {
			isAvailable := false
			for _, condition := range catalog.Status.Conditions {
				if condition.Type == "Serving" && condition.Status == "True" {
					isAvailable = true
					break
				}
			}
			if isAvailable {
				availableCount++
			} else {
				errorCount++
			}
		}

		if errorCount > 0 {
			fmt.Printf("  [?] ClusterCatalogs: %d available, %d with errors\n", availableCount, errorCount)
		} else {
			fmt.Printf("  [+] ClusterCatalogs: %d available\n", len(catalogs))
		}
	}

	// Check ClusterExtensions with detailed status
	extensions, err := r.olmClient.GetClusterExtensions(r.ctx)
	if err != nil {
		fmt.Printf("  [!] ClusterExtensions: Unable to access (%v)\n", err)
	} else {
		installedCount := 0
		failedCount := 0
		for _, extension := range extensions {
			isInstalled := false
			for _, condition := range extension.Status.Conditions {
				if condition.Type == "Installed" {
					if condition.Status == "True" {
						isInstalled = true
					} else {
						failedCount++
					}
					break
				}
			}
			if isInstalled {
				installedCount++
			}
		}

		if failedCount > 0 {
			fmt.Printf("  [?] ClusterExtensions: %d installed, %d failed\n", installedCount, failedCount)
		} else {
			fmt.Printf("  [+] ClusterExtensions: %d installed\n", len(extensions))
		}
	}

	return nil
}

// diagnoseCatalog provides detailed troubleshooting information for a ClusterCatalog
func (r *REPL) diagnoseCatalog(name string) error {
	fmt.Printf("Diagnosing ClusterCatalog '%s'...\n\n", name)

	// Get the catalog
	catalog, err := r.olmClient.GetClusterCatalog(r.ctx, name)
	if err != nil {
		return fmt.Errorf("failed to get ClusterCatalog '%s': %w", name, err)
	}

	// Basic information
	fmt.Printf("Basic Information:\n")
	fmt.Printf("  Name:         %s\n", catalog.Name)
	fmt.Printf("  Created:      %s\n", catalog.CreationTimestamp.Format("2006-01-02 15:04:05"))
	fmt.Printf("  Source Type:  %s\n", catalog.Spec.Source.Type)
	if catalog.Spec.Source.Image != nil {
		fmt.Printf("  Source Image: %s\n", catalog.Spec.Source.Image.Ref)
	}
	fmt.Println()

	// Analyze conditions
	fmt.Printf("Status Analysis:\n")
	if len(catalog.Status.Conditions) == 0 {
		fmt.Printf("  [?] No status conditions found - catalog may be initializing\n")
	} else {
		hasServing := false
		for _, condition := range catalog.Status.Conditions {
			status := "[!]"
			if condition.Status == "True" {
				status = "[+]"
			} else if condition.Status == "Unknown" {
				status = "?"
			}

			fmt.Printf("  %s %s: %s\n", status, condition.Type, condition.Status)
			if condition.Reason != "" {
				fmt.Printf("    Reason: %s\n", condition.Reason)
			}
			if condition.Message != "" {
				fmt.Printf("    Message: %s\n", condition.Message)
			}
			if condition.LastTransitionTime.Time.After(catalog.CreationTimestamp.Time) {
				fmt.Printf("    Last Updated: %s\n", condition.LastTransitionTime.Format("2006-01-02 15:04:05"))
			}

			if condition.Type == "Serving" {
				hasServing = true
			}
		}

		if !hasServing {
			fmt.Printf("  [?] No 'Serving' condition found - catalog may still be processing\n")
		}
	}
	fmt.Println()

	// Check URLs
	if catalog.Status.URLs != nil {
		fmt.Printf("Service URLs:\n")
		fmt.Printf("  Base URL: %s\n", catalog.Status.URLs.Base)
		fmt.Println()
	} else {
		fmt.Printf("Service URLs:\n")
		fmt.Printf("  [!] No URLs available - catalog is not ready\n")
		fmt.Println()
	}

	// Check related events
	if err := r.checkResourceEvents("ClusterCatalog", name, ""); err != nil {
		fmt.Printf("Events: Unable to fetch (%v)\n", err)
	}

	// Check catalogd status
	fmt.Printf("Catalogd Status:\n")
	catalogdNamespace, err := r.findCatalogdNamespace()
	if err != nil {
		fmt.Printf("  [!] Catalogd not found: %v\n", err)
	} else {
		pods, err := r.k8sClient.CoreV1().Pods(catalogdNamespace).List(r.ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/name=catalogd",
		})
		if err != nil {
			fmt.Printf("  [!] Error checking catalogd pods: %v\n", err)
		} else if len(pods.Items) == 0 {
			fmt.Printf("  [!] No catalogd pods found in %s\n", catalogdNamespace)
		} else {
			readyPods := 0
			for _, pod := range pods.Items {
				for _, condition := range pod.Status.Conditions {
					if condition.Type == "Ready" && condition.Status == "True" {
						readyPods++
						break
					}
				}
			}
			if readyPods == 0 {
				fmt.Printf("  [!] Catalogd pods exist but none are ready (%d pods in %s)\n", len(pods.Items), catalogdNamespace)
			} else {
				fmt.Printf("  [+] Catalogd is healthy (%d/%d pods ready in %s)\n", readyPods, len(pods.Items), catalogdNamespace)
			}
		}
	}


	return nil
}

// diagnoseExtension provides detailed troubleshooting information for a ClusterExtension
func (r *REPL) diagnoseExtension(name string) error {
	fmt.Printf("Diagnosing ClusterExtension '%s'...\n\n", name)

	// Get the extension
	extension, err := r.olmClient.GetClusterExtension(r.ctx, name)
	if err != nil {
		return fmt.Errorf("failed to get ClusterExtension '%s': %w", name, err)
	}

	// Basic information
	fmt.Printf("Basic Information:\n")
	fmt.Printf("  Name:     %s\n", extension.Name)
	fmt.Printf("  Created:  %s\n", extension.CreationTimestamp.Format("2006-01-02 15:04:05"))

	if extension.Spec.Source.Catalog != nil {
		fmt.Printf("  Package:  %s\n", extension.Spec.Source.Catalog.PackageName)
		if extension.Spec.Source.Catalog.Version != "" {
			fmt.Printf("  Version:  %s\n", extension.Spec.Source.Catalog.Version)
		}
		if len(extension.Spec.Source.Catalog.Channels) > 0 {
			fmt.Printf("  Channels: %s\n", strings.Join(extension.Spec.Source.Catalog.Channels, ", "))
		}
	}
	fmt.Println()

	// Analyze conditions
	fmt.Printf("Status Analysis:\n")
	if len(extension.Status.Conditions) == 0 {
		fmt.Printf("  [?] No status conditions found - extension may be initializing\n")
	} else {
		hasInstalled := false
		for _, condition := range extension.Status.Conditions {
			status := "[!]"
			if condition.Status == "True" {
				status = "[+]"
			} else if condition.Status == "Unknown" {
				status = "?"
			}

			fmt.Printf("  %s %s: %s\n", status, condition.Type, condition.Status)
			if condition.Reason != "" {
				fmt.Printf("    Reason: %s\n", condition.Reason)
			}
			if condition.Message != "" {
				fmt.Printf("    Message: %s\n", condition.Message)
			}
			if condition.LastTransitionTime.Time.After(extension.CreationTimestamp.Time) {
				fmt.Printf("    Last Updated: %s\n", condition.LastTransitionTime.Format("2006-01-02 15:04:05"))
			}

			if condition.Type == "Installed" {
				hasInstalled = true
			}
		}

		if !hasInstalled {
			fmt.Printf("  [?] No 'Installed' condition found - extension may still be processing\n")
		}
	}
	fmt.Println()

	// Show installed bundle information
	if extension.Status.Install != nil {
		fmt.Printf("Installed Bundle:\n")
		fmt.Printf("  Name:    %s\n", extension.Status.Install.Bundle.Name)
		fmt.Printf("  Version: %s\n", extension.Status.Install.Bundle.Version)
		fmt.Println()
	} else {
		fmt.Printf("Installed Bundle:\n")
		fmt.Printf("  [!] No bundle installed yet\n")
		fmt.Println()
	}

	// Check related events
	if err := r.checkResourceEvents("ClusterExtension", name, ""); err != nil {
		fmt.Printf("Events: Unable to fetch (%v)\n", err)
	}

	// Check operator-controller status
	fmt.Printf("Operator Controller Status:\n")
	operatorControllerNamespace, err := r.findOperatorControllerNamespace()
	if err != nil {
		fmt.Printf("  [!] Operator Controller not found: %v\n", err)
	} else {
		pods, err := r.k8sClient.CoreV1().Pods(operatorControllerNamespace).List(r.ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/name=operator-controller",
		})
		if err != nil {
			fmt.Printf("  [!] Error checking operator-controller pods: %v\n", err)
		} else if len(pods.Items) == 0 {
			fmt.Printf("  [!] No operator-controller pods found in %s\n", operatorControllerNamespace)
		} else {
			readyPods := 0
			for _, pod := range pods.Items {
				for _, condition := range pod.Status.Conditions {
					if condition.Type == "Ready" && condition.Status == "True" {
						readyPods++
						break
					}
				}
			}
			if readyPods == 0 {
				fmt.Printf("  [!] Operator Controller pods exist but none are ready (%d pods in %s)\n", len(pods.Items), operatorControllerNamespace)
			} else {
				fmt.Printf("  [+] Operator Controller is healthy (%d/%d pods ready in %s)\n", readyPods, len(pods.Items), operatorControllerNamespace)
			}
		}
	}


	return nil
}

// checkResourceEvents fetches and displays recent events for a resource
func (r *REPL) checkResourceEvents(kind, name, namespace string) error {
	fmt.Printf("Recent Events:\n")

	// Determine which namespace to search - use all namespaces for cluster-scoped resources
	listOptions := metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.kind=%s,involvedObject.name=%s", kind, name),
	}

	var events *corev1.EventList
	var err error

	if namespace == "" {
		// For cluster-scoped resources, we need to check all namespaces
		// Start with common namespaces where OLM events might appear
		checkNamespaces := []string{"default", "olmv1-system", "openshift-catalogd", "openshift-operator-lifecycle-manager"}
		
		for _, ns := range checkNamespaces {
			events, err = r.k8sClient.CoreV1().Events(ns).List(r.ctx, listOptions)
			if err == nil && len(events.Items) > 0 {
				break
			}
		}
		
		// If no events found, try all namespaces (this is expensive but thorough)
		if err != nil || len(events.Items) == 0 {
			allNamespaces, nsErr := r.k8sClient.CoreV1().Namespaces().List(r.ctx, metav1.ListOptions{})
			if nsErr == nil {
				for _, ns := range allNamespaces.Items {
					events, err = r.k8sClient.CoreV1().Events(ns.Name).List(r.ctx, listOptions)
					if err == nil && len(events.Items) > 0 {
						break
					}
				}
			}
		}
	} else {
		events, err = r.k8sClient.CoreV1().Events(namespace).List(r.ctx, listOptions)
	}

	if err != nil {
		return err
	}

	if len(events.Items) == 0 {
		fmt.Printf("  No recent events found\n")
	} else {
		// Sort events by last timestamp (most recent first)
		eventItems := events.Items
		for i := 0; i < len(eventItems)-1; i++ {
			for j := i + 1; j < len(eventItems); j++ {
				if eventItems[i].LastTimestamp.Before(&eventItems[j].LastTimestamp) {
					eventItems[i], eventItems[j] = eventItems[j], eventItems[i]
				}
			}
		}

		// Show up to 10 most recent events
		maxEvents := 10
		if len(eventItems) < maxEvents {
			maxEvents = len(eventItems)
		}

		for i := 0; i < maxEvents; i++ {
			event := eventItems[i]
			eventType := "[i]"
			if event.Type == "Warning" {
				eventType = "[?]"
			} else if event.Type == "Error" {
				eventType = "[!]"
			}

			fmt.Printf("  %s [%s] %s: %s\n", 
				eventType,
				event.LastTimestamp.Format("15:04:05"),
				event.Reason,
				event.Message,
			)
		}
	}
	fmt.Println()

	return nil
}

// findCatalogdNamespace searches for the namespace containing catalogd deployment
// This is a copy of the logic from portforward package to avoid dependency issues
func (r *REPL) findCatalogdNamespace() (string, error) {
	// Common catalogd namespaces to check, in order of preference
	candidateNamespaces := []string{
		"openshift-catalogd",         // OpenShift default
		"olmv1-system",               // Upstream default
		"operator-lifecycle-manager", // Alternative
	}

	// First, try the common namespaces
	for _, ns := range candidateNamespaces {
		pods, err := r.k8sClient.CoreV1().Pods(ns).List(r.ctx, metav1.ListOptions{
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
	namespaces, err := r.k8sClient.CoreV1().Namespaces().List(r.ctx, metav1.ListOptions{})
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

		pods, err := r.k8sClient.CoreV1().Pods(ns.Name).List(r.ctx, metav1.ListOptions{
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

// findOperatorControllerNamespace searches for the namespace containing operator-controller deployment
func (r *REPL) findOperatorControllerNamespace() (string, error) {
	// Common operator-controller namespaces to check, in order of preference
	candidateNamespaces := []string{
		"openshift-operator-lifecycle-manager", // OpenShift default
		"olmv1-system",                         // Upstream default
		"operator-lifecycle-manager",           // Alternative
	}

	// First, try the common namespaces
	for _, ns := range candidateNamespaces {
		pods, err := r.k8sClient.CoreV1().Pods(ns).List(r.ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/name=operator-controller",
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
	namespaces, err := r.k8sClient.CoreV1().Namespaces().List(r.ctx, metav1.ListOptions{})
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

		pods, err := r.k8sClient.CoreV1().Pods(ns.Name).List(r.ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/name=operator-controller",
		})
		if err != nil {
			continue
		}

		if len(pods.Items) > 0 {
			return ns.Name, nil
		}
	}

	return "", fmt.Errorf("no operator-controller pods found in any namespace")
}

// refreshCache invalidates all cached data and forces fresh fetches
func (r *REPL) refreshCache() error {
	r.cache.InvalidateAll()
	fmt.Println("Cache refreshed. Tab completion will use fresh data.")
	return nil
}

// clearScreen clears the terminal screen
func (r *REPL) clearScreen() error {
	// ANSI escape sequence to clear screen and move cursor to top-left
	fmt.Print("\033[2J\033[H")
	return nil
}

// handleCatalogCommands processes catalog-related commands
func (r *REPL) handleCatalogCommands(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("catalog command requires a subcommand. Use 'help' for more info")
	}

	switch args[0] {
	case "list", "ls":
		return r.listCatalogs()
	case "get":
		if len(args) < 2 {
			return fmt.Errorf("'get' requires a catalog name")
		}
		return r.getCatalog(args[1])
	case "packages":
		if len(args) < 2 {
			return fmt.Errorf("'packages' requires a catalog name")
		}
		return r.listPackagesInCatalog(args[1])
	case "package":
		if len(args) < 3 {
			return fmt.Errorf("'package' requires a catalog name and package name")
		}
		return r.getPackageDetails(args[1], args[2])
	case "search":
		if len(args) < 3 {
			return fmt.Errorf("'search' requires a catalog name and search term")
		}
		return r.searchPackages(args[1], args[2])
	default:
		return fmt.Errorf("unknown catalog command: %s", args[0])
	}
}

// handleExtensionCommands processes extension-related commands
func (r *REPL) handleExtensionCommands(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("extension command requires a subcommand. Use 'help' for more info")
	}

	switch args[0] {
	case "list", "ls":
		return r.listExtensions()
	case "get":
		if len(args) < 2 {
			return fmt.Errorf("'get' requires an extension name")
		}
		return r.getExtension(args[1])
	case "install-experimental":
		// Check for help first
		if len(args) >= 2 && (args[1] == "--help" || args[1] == "-h" || args[1] == "help") {
			return r.showInstallHelp()
		}
		if len(args) < 3 {
			fmt.Println("Error: 'install-experimental' requires a catalog name and package name")
			fmt.Println()
			return r.showInstallHelp()
		}
		return r.installExtensionExperimental(args[1], args[2], args[3:])
	case "uninstall-experimental":
		// Check for help first
		if len(args) >= 2 && (args[1] == "--help" || args[1] == "-h" || args[1] == "help") {
			return r.showUninstallHelp()
		}
		if len(args) < 2 {
			fmt.Println("Error: 'uninstall-experimental' requires an extension name")
			fmt.Println()
			return r.showUninstallHelp()
		}
		return r.uninstallExtensionExperimental(args[1], args[2:])
	default:
		return fmt.Errorf("unknown extension command: %s", args[0])
	}
}

// handleDiagnoseCommands processes diagnose-related commands
func (r *REPL) handleDiagnoseCommands(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("diagnose command requires a subcommand. Use 'help' for more info")
	}

	switch args[0] {
	case "catalog":
		if len(args) < 2 {
			return fmt.Errorf("'diagnose catalog' requires a catalog name")
		}
		return r.diagnoseCatalog(args[1])
	case "extension":
		if len(args) < 2 {
			return fmt.Errorf("'diagnose extension' requires an extension name")
		}
		return r.diagnoseExtension(args[1])
	default:
		return fmt.Errorf("unknown diagnose command: %s. Available: catalog, extension", args[0])
	}
}

// listCatalogs displays all ClusterCatalogs
func (r *REPL) listCatalogs() error {
	catalogs, err := r.olmClient.GetClusterCatalogs(r.ctx)
	if err != nil {
		return err
	}

	if len(catalogs) == 0 {
		fmt.Println("No ClusterCatalogs found")
		return nil
	}

	fmt.Printf("Found %d ClusterCatalog(s):\n\n", len(catalogs))
	fmt.Printf("%-20s %-20s %-15s %-10s\n", "NAME", "SOURCE", "AVAILABILITY", "AGE")
	fmt.Println(strings.Repeat("-", 70))

	for _, catalog := range catalogs {
		availability := "Unknown"
		if len(catalog.Status.Conditions) > 0 {
			for _, condition := range catalog.Status.Conditions {
				if condition.Type == "Serving" {
					if condition.Status == "True" {
						availability = "Available"
					} else {
						availability = "Unavailable"
					}
					break
				}
			}
		}

		age := "Unknown"
		if !catalog.CreationTimestamp.IsZero() {
			age = fmt.Sprintf("%s", catalog.CreationTimestamp.Time.Format("2006-01-02"))
		}

		source := "Unknown"
		if catalog.Spec.Source.Type != "" {
			source = string(catalog.Spec.Source.Type)
		}

		fmt.Printf("%-20s %-20s %-15s %-10s\n",
			catalog.Name,
			source,
			availability,
			age,
		)
	}

	return nil
}

// getCatalog displays details for a specific ClusterCatalog
func (r *REPL) getCatalog(name string) error {
	catalog, err := r.olmClient.GetClusterCatalog(r.ctx, name)
	if err != nil {
		return err
	}

	fmt.Printf("ClusterCatalog: %s\n\n", catalog.Name)
	fmt.Printf("Source Type:     %s\n", catalog.Spec.Source.Type)
	if catalog.Spec.Source.Image != nil {
		fmt.Printf("Source Image:    %s\n", catalog.Spec.Source.Image.Ref)
	}
	fmt.Printf("Created:         %s\n", catalog.CreationTimestamp.Format("2006-01-02 15:04:05"))

	if len(catalog.Status.Conditions) > 0 {
		fmt.Printf("\nStatus:\n")
		hasFailures := false
		for _, condition := range catalog.Status.Conditions {
			status := "[!]"
			if condition.Status == "True" {
				status = "[+]"
			} else if condition.Status == "Unknown" {
				status = "?"
			} else {
				hasFailures = true
			}

			fmt.Printf("  %s %s: %s", status, condition.Type, condition.Status)
			if condition.Reason != "" {
				fmt.Printf(" (%s)", condition.Reason)
			}
			fmt.Printf("\n")
			
			if condition.Message != "" {
				fmt.Printf("    Message: %s\n", condition.Message)
			}
		}
		
		if hasFailures {
			fmt.Printf("\nTIP: For detailed troubleshooting, run: diagnose catalog %s\n", name)
		}
	} else {
		fmt.Printf("\nStatus:\n")
		fmt.Printf("  [?] No status conditions found - catalog may be initializing\n")
		fmt.Printf("\nTIP: For detailed troubleshooting, run: diagnose catalog %s\n", name)
	}

	return nil
}

// setupPortForwardIfNeeded sets up port forwarding for catalogd if not already done
func (r *REPL) setupPortForwardIfNeeded() error {
	if r.portForward != nil {
		return nil // Already set up
	}

	r.portForward = portforward.NewPortForwarder(r.config, r.k8sClient)
	if err := r.portForward.Start(); err != nil {
		return fmt.Errorf("failed to start port forwarding: %w", err)
	}
	return nil
}

// CleanupPortForward stops port forwarding if it was set up (exported for cleanup)
func (r *REPL) CleanupPortForward() {
	if r.portForward != nil {
		r.portForward.Stop()
		r.portForward = nil
	}
}

// listPackagesInCatalog lists packages available in a specific catalog
func (r *REPL) listPackagesInCatalog(catalogName string) error {
	fmt.Printf("Packages in catalog '%s':\n", catalogName)
	fmt.Println("Querying catalogd service...")

	// Set up port forwarding if needed for non-interactive use
	if err := r.setupPortForwardIfNeeded(); err != nil {
		return err
	}
	fmt.Println()

	// Get the catalog to get its base URL
	catalog, err := r.olmClient.GetClusterCatalog(r.ctx, catalogName)
	if err != nil {
		return fmt.Errorf("failed to get catalog: %w", err)
	}

	if catalog.Status.URLs == nil {
		return fmt.Errorf("catalog %s has no status URLs - catalog may not be ready", catalogName)
	}

	// Use port-forwarded URL if available
	baseURL := catalog.Status.URLs.Base
	if r.portForward != nil {
		baseURL = r.portForward.GetLocalURL() + "/catalogs/" + catalogName
	}

	packages, err := r.catalogdClient.GetPackages(r.ctx, catalogName, baseURL)
	if err != nil {
		return fmt.Errorf("failed to get packages from catalog: %w", err)
	}

	if len(packages) == 0 {
		fmt.Println("No packages found in this catalog")
		return nil
	}

	fmt.Printf("Found %d package(s):\n\n", len(packages))
	fmt.Printf("%-50s %-20s %-8s\n", "NAME", "DEFAULT CHANNEL", "CHANNELS")
	fmt.Println(strings.Repeat("-", 82))

	for _, pkg := range packages {
		// Truncate package name if too long
		displayName := pkg.Name
		if len(displayName) > 48 {
			displayName = displayName[:45] + "..."
		}

		// Truncate default channel if too long
		displayChannel := pkg.DefaultChannel
		if len(displayChannel) > 18 {
			displayChannel = displayChannel[:15] + "..."
		}

		fmt.Printf("%-50s %-20s %-8d\n",
			displayName,
			displayChannel,
			len(pkg.Channels),
		)
	}

	fmt.Println()
	fmt.Printf("Use 'catalogs package %s <package-name>' to get detailed package information\n", catalogName)
	return nil
}

// getPackageDetails displays detailed information about a specific package
func (r *REPL) getPackageDetails(catalogName, packageName string) error {
	fmt.Printf("Package details for '%s' in catalog '%s':\n", packageName, catalogName)
	fmt.Println("Querying catalogd service...")

	// Set up port forwarding if needed for non-interactive use
	if err := r.setupPortForwardIfNeeded(); err != nil {
		return err
	}
	fmt.Println()

	// Get the catalog to get its base URL
	catalog, err := r.olmClient.GetClusterCatalog(r.ctx, catalogName)
	if err != nil {
		return fmt.Errorf("failed to get catalog: %w", err)
	}

	if catalog.Status.URLs == nil {
		return fmt.Errorf("catalog %s has no status URLs - catalog may not be ready", catalogName)
	}

	// Use port-forwarded URL if available
	baseURL := catalog.Status.URLs.Base
	if r.portForward != nil {
		baseURL = r.portForward.GetLocalURL() + "/catalogs/" + catalogName
	}

	pkg, err := r.catalogdClient.GetPackage(r.ctx, catalogName, packageName, baseURL)
	if err != nil {
		return fmt.Errorf("failed to get package details: %w", err)
	}

	fmt.Printf("Name:            %s\n", pkg.Name)
	fmt.Printf("Default Channel: %s\n", pkg.DefaultChannel)

	if len(pkg.Channels) > 0 {
		fmt.Printf("\nChannels (%d):\n", len(pkg.Channels))
		for _, channel := range pkg.Channels {
			fmt.Printf("  %s (%d bundles)\n", channel.Name, len(channel.Entries))
			if len(channel.Entries) > 0 {
				// Show the latest bundle
				latest := channel.Entries[0]
				fmt.Printf("    Latest: %s (version %s)\n", latest.Name, latest.Version)
			}
		}
	}

	return nil
}

// searchPackages searches for packages in a catalog
func (r *REPL) searchPackages(catalogName, searchTerm string) error {
	fmt.Printf("Searching for '%s' in catalog '%s':\n", searchTerm, catalogName)
	fmt.Println("Querying catalogd service...")

	// Set up port forwarding if needed for non-interactive use
	if err := r.setupPortForwardIfNeeded(); err != nil {
		return err
	}
	fmt.Println()

	// Get the catalog to get its base URL
	catalog, err := r.olmClient.GetClusterCatalog(r.ctx, catalogName)
	if err != nil {
		return fmt.Errorf("failed to get catalog: %w", err)
	}

	if catalog.Status.URLs == nil {
		return fmt.Errorf("catalog %s has no status URLs - catalog may not be ready", catalogName)
	}

	// Use port-forwarded URL if available
	baseURL := catalog.Status.URLs.Base
	if r.portForward != nil {
		baseURL = r.portForward.GetLocalURL() + "/catalogs/" + catalogName
	}

	packages, err := r.catalogdClient.SearchPackages(r.ctx, catalogName, searchTerm, baseURL)
	if err != nil {
		return fmt.Errorf("failed to search packages: %w", err)
	}

	if len(packages) == 0 {
		fmt.Printf("No packages found matching '%s'\n", searchTerm)
		return nil
	}

	fmt.Printf("Found %d matching package(s):\n\n", len(packages))
	fmt.Printf("%-50s %-20s %-8s\n", "NAME", "DEFAULT CHANNEL", "CHANNELS")
	fmt.Println(strings.Repeat("-", 82))

	for _, pkg := range packages {
		// Truncate package name if too long
		displayName := pkg.Name
		if len(displayName) > 48 {
			displayName = displayName[:45] + "..."
		}

		// Truncate default channel if too long
		displayChannel := pkg.DefaultChannel
		if len(displayChannel) > 18 {
			displayChannel = displayChannel[:15] + "..."
		}

		fmt.Printf("%-50s %-20s %-8d\n",
			displayName,
			displayChannel,
			len(pkg.Channels),
		)
	}

	return nil
}

// listExtensions displays all ClusterExtensions
func (r *REPL) listExtensions() error {
	extensions, err := r.olmClient.GetClusterExtensions(r.ctx)
	if err != nil {
		return err
	}

	if len(extensions) == 0 {
		fmt.Println("No ClusterExtensions found")
		return nil
	}

	fmt.Printf("Found %d ClusterExtension(s):\n\n", len(extensions))
	fmt.Printf("%-20s %-20s %-15s %-15s %-10s\n", "NAME", "PACKAGE", "VERSION", "STATUS", "AGE")
	fmt.Println(strings.Repeat("-", 85))

	for _, extension := range extensions {
		status := "Unknown"
		if len(extension.Status.Conditions) > 0 {
			for _, condition := range extension.Status.Conditions {
				if condition.Type == "Installed" {
					if condition.Status == "True" {
						status = "Installed"
					} else {
						status = "Failed"
					}
					break
				}
			}
		}

		age := "Unknown"
		if !extension.CreationTimestamp.IsZero() {
			age = fmt.Sprintf("%s", extension.CreationTimestamp.Time.Format("2006-01-02"))
		}

		version := "Unknown"
		if extension.Status.Install != nil && extension.Status.Install.Bundle.Version != "" {
			version = extension.Status.Install.Bundle.Version
		}

		packageName := "Unknown"
		if extension.Spec.Source.Catalog != nil && extension.Spec.Source.Catalog.PackageName != "" {
			packageName = extension.Spec.Source.Catalog.PackageName
		}

		fmt.Printf("%-20s %-20s %-15s %-15s %-10s\n",
			extension.Name,
			packageName,
			version,
			status,
			age,
		)
	}

	return nil
}

// getExtension displays details for a specific ClusterExtension
func (r *REPL) getExtension(name string) error {
	extension, err := r.olmClient.GetClusterExtension(r.ctx, name)
	if err != nil {
		return err
	}

	fmt.Printf("ClusterExtension: %s\n\n", extension.Name)

	if extension.Spec.Source.Catalog != nil {
		fmt.Printf("Package:         %s\n", extension.Spec.Source.Catalog.PackageName)
		if extension.Spec.Source.Catalog.Version != "" {
			fmt.Printf("Version:         %s\n", extension.Spec.Source.Catalog.Version)
		}
		if len(extension.Spec.Source.Catalog.Channels) > 0 {
			fmt.Printf("Channels:        %s\n", strings.Join(extension.Spec.Source.Catalog.Channels, ", "))
		}
	}

	fmt.Printf("Created:         %s\n", extension.CreationTimestamp.Format("2006-01-02 15:04:05"))

	if extension.Status.Install != nil {
		fmt.Printf("\nInstalled Bundle:\n")
		fmt.Printf("  Name:          %s\n", extension.Status.Install.Bundle.Name)
		fmt.Printf("  Version:       %s\n", extension.Status.Install.Bundle.Version)
	}

	if len(extension.Status.Conditions) > 0 {
		fmt.Printf("\nStatus:\n")
		hasFailures := false
		for _, condition := range extension.Status.Conditions {
			status := "[!]"
			if condition.Status == "True" {
				status = "[+]"
			} else if condition.Status == "Unknown" {
				status = "?"
			} else {
				hasFailures = true
			}

			fmt.Printf("  %s %s: %s", status, condition.Type, condition.Status)
			if condition.Reason != "" {
				fmt.Printf(" (%s)", condition.Reason)
			}
			fmt.Printf("\n")
			
			if condition.Message != "" {
				fmt.Printf("    Message: %s\n", condition.Message)
			}
		}
		
		if hasFailures {
			fmt.Printf("\nTIP: For detailed troubleshooting, run: diagnose extension %s\n", name)
		}
	} else {
		fmt.Printf("\nStatus:\n")
		fmt.Printf("  [?] No status conditions found - extension may be initializing\n")
		fmt.Printf("\nTIP: For detailed troubleshooting, run: diagnose extension %s\n", name)
	}

	return nil
}

// installExtensionExperimental installs a ClusterExtension with cluster-admin ServiceAccount
func (r *REPL) installExtensionExperimental(catalogName, packageName string, options []string) error {
	// Parse options
	namespace := "opconsh-test"
	var version, channel, extensionName string
	skipConfirmation := false

	for i := 0; i < len(options); i++ {
		switch options[i] {
		case "--help", "-h", "help":
			return r.showInstallHelp()
		case "--namespace":
			if i+1 < len(options) {
				namespace = options[i+1]
				i++
			}
		case "--version":
			if i+1 < len(options) {
				version = options[i+1]
				i++
			}
		case "--channel":
			if i+1 < len(options) {
				channel = options[i+1]
				i++
			}
		case "--name":
			if i+1 < len(options) {
				extensionName = options[i+1]
				i++
			}
		case "--yes":
			skipConfirmation = true
		}
	}

	// Default extension name
	if extensionName == "" {
		extensionName = packageName
	}

	// Display security warnings
	fmt.Println()
	fmt.Println("⚠️  EXPERIMENTAL COMMAND - TESTING ONLY ⚠️")
	fmt.Println()
	fmt.Println("This command creates a ServiceAccount with cluster-admin privileges.")
	fmt.Println("This grants FULL CLUSTER ACCESS to the installed extension.")
	fmt.Println()
	fmt.Println("SECURITY RISKS:")
	fmt.Println("• Extension has unrestricted access to all cluster resources")
	fmt.Println("• Can read/modify secrets, RBAC, and sensitive data")
	fmt.Println("• Suitable ONLY for testing and development")
	fmt.Println()
	fmt.Printf("Target namespace: %s\n", namespace)
	fmt.Printf("Extension name: %s\n", extensionName)
	fmt.Printf("Package: %s (from catalog: %s)\n", packageName, catalogName)
	if version != "" {
		fmt.Printf("Version: %s\n", version)
	}
	if channel != "" {
		fmt.Printf("Channel: %s\n", channel)
	}
	fmt.Println()

	// Confirmation prompt
	if !skipConfirmation {
		fmt.Print("Continue? (type 'yes' to confirm): ")
		var response string
		fmt.Scanln(&response)
		if response != "yes" {
			fmt.Println("Installation cancelled.")
			return nil
		}
	}

	// Pre-flight checks
	fmt.Println("Performing pre-flight checks...")

	// Check if catalog exists
	_, err := r.olmClient.GetClusterCatalog(r.ctx, catalogName)
	if err != nil {
		return fmt.Errorf("catalog '%s' not found: %w", catalogName, err)
	}
	fmt.Printf("[+] Catalog '%s' exists\n", catalogName)

	// Check if extension already exists
	_, err = r.olmClient.GetClusterExtension(r.ctx, extensionName)
	if err == nil {
		return fmt.Errorf("ClusterExtension '%s' already exists", extensionName)
	}
	fmt.Printf("[+] Extension name '%s' is available\n", extensionName)

	// Create resources
	fmt.Println()
	fmt.Println("Creating resources...")

	// Create namespace if needed
	if namespace != "default" {
		fmt.Printf("[+] Creating namespace '%s'...\n", namespace)
		if err := r.olmClient.CreateNamespace(r.ctx, namespace); err != nil {
			return fmt.Errorf("failed to create namespace: %w", err)
		}
	}

	// Create ServiceAccount
	serviceAccountName := fmt.Sprintf("opconsh-%s", extensionName)
	fmt.Printf("[+] Creating ServiceAccount '%s' in namespace '%s'...\n", serviceAccountName, namespace)
	if err := r.olmClient.CreateServiceAccount(r.ctx, namespace, serviceAccountName); err != nil {
		return fmt.Errorf("failed to create ServiceAccount: %w", err)
	}

	// Create ClusterRoleBinding
	clusterRoleBindingName := fmt.Sprintf("opconsh-%s-cluster-admin", extensionName)
	fmt.Printf("[+] Creating ClusterRoleBinding '%s' with cluster-admin privileges...\n", clusterRoleBindingName)
	if err := r.olmClient.CreateClusterRoleBinding(r.ctx, clusterRoleBindingName, serviceAccountName, namespace); err != nil {
		return fmt.Errorf("failed to create ClusterRoleBinding: %w", err)
	}

	// Create ClusterExtension
	fmt.Printf("[+] Creating ClusterExtension '%s'...\n", extensionName)
	extension := &olmv1.ClusterExtension{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "olm.operatorframework.io/v1",
			Kind:       "ClusterExtension",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: extensionName,
			Labels: map[string]string{
				"created-by": "opconsh",
				"purpose":    "experimental-install",
			},
		},
		Spec: olmv1.ClusterExtensionSpec{
			Namespace: namespace,
			ServiceAccount: olmv1.ServiceAccountReference{
				Name: serviceAccountName,
			},
			Source: olmv1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &olmv1.CatalogFilter{
					PackageName: packageName,
				},
			},
		},
	}

	// Set version if specified
	if version != "" {
		extension.Spec.Source.Catalog.Version = version
	}

	// Set channel if specified
	if channel != "" {
		extension.Spec.Source.Catalog.Channels = []string{channel}
	}

	if err := r.olmClient.CreateClusterExtension(r.ctx, extension); err != nil {
		return fmt.Errorf("failed to create ClusterExtension: %w", err)
	}

	fmt.Println()
	fmt.Println("✅ Experimental extension installation initiated!")
	fmt.Println()
	fmt.Printf("Monitor installation status with: extensions get %s\n", extensionName)
	fmt.Printf("Diagnose any issues with: diagnose extension %s\n", extensionName)
	fmt.Println()
	fmt.Println("⚠️  REMEMBER: This extension has cluster-admin privileges!")

	return nil
}

// showInstallHelp displays help for the install-experimental command
func (r *REPL) showInstallHelp() error {
	fmt.Println("install-experimental - Install a ClusterExtension with cluster-admin privileges")
	fmt.Println()
	fmt.Println("⚠️  WARNING: This is for TESTING ONLY and grants full cluster access!")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  extensions install-experimental <catalog> <package> [options]")
	fmt.Println("  ext install-experimental <catalog> <package> [options]")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  ext install-experimental operatorhubio prometheus-operator")
	fmt.Println("  ext install-experimental operatorhubio grafana-operator --namespace monitoring --name my-grafana")
	fmt.Println("  ext install-experimental operatorhubio prometheus-operator --version 0.68.0 --channel stable --yes")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  --namespace <name>    Target namespace for extension (default: opconsh-test)")
	fmt.Println("  --name <name>         Custom name for ClusterExtension (default: package name)")
	fmt.Println("  --version <version>   Specific version to install")
	fmt.Println("  --channel <channel>   Specific channel to use")
	fmt.Println("  --yes                 Skip confirmation prompt")
	fmt.Println("  --help, -h, help      Show this help message")
	fmt.Println()
	fmt.Println("Security Notice:")
	fmt.Println("This command creates:")
	fmt.Println("• A ClusterExtension in the specified namespace")
	fmt.Println("• A ServiceAccount with cluster-admin ClusterRoleBinding")
	fmt.Println("• Grants UNRESTRICTED access to ALL cluster resources")
	fmt.Println()
	fmt.Println("Use 'uninstall-experimental <extension-name>' to remove the extension and cleanup.")
	fmt.Println()
	return nil
}

// uninstallExtensionExperimental uninstalls a ClusterExtension and cleans up associated RBAC resources
func (r *REPL) uninstallExtensionExperimental(extensionName string, options []string) error {
	// Parse options
	cleanupRBAC := true
	cleanupNamespace := true
	skipConfirmation := false

	for i := 0; i < len(options); i++ {
		switch options[i] {
		case "--help", "-h", "help":
			return r.showUninstallHelp()
		case "--keep-rbac":
			cleanupRBAC = false
		case "--keep-namespace":
			cleanupNamespace = false
		case "--yes":
			skipConfirmation = true
		}
	}

	fmt.Println()
	fmt.Println("⚠️  EXPERIMENTAL UNINSTALL ⚠️")
	fmt.Println()

	// Get the extension to show details and determine what was created
	extension, err := r.olmClient.GetClusterExtension(r.ctx, extensionName)
	if err != nil {
		return fmt.Errorf("failed to get ClusterExtension '%s': %w", extensionName, err)
	}

	// Check if this was created by opconsh
	if extension.Labels["created-by"] != "opconsh" || extension.Labels["purpose"] != "experimental-install" {
		return fmt.Errorf("ClusterExtension '%s' was not created by opconsh experimental install", extensionName)
	}

	fmt.Printf("Extension: %s\n", extensionName)
	if extension.Spec.Source.Catalog != nil {
		fmt.Printf("Package: %s\n", extension.Spec.Source.Catalog.PackageName)
	}
	fmt.Printf("Namespace: %s\n", extension.Spec.Namespace)
	if extension.Spec.ServiceAccount.Name != "" {
		fmt.Printf("ServiceAccount: %s\n", extension.Spec.ServiceAccount.Name)
	}
	fmt.Println()

	fmt.Println("This will remove:")
	fmt.Printf("• ClusterExtension '%s'\n", extensionName)
	
	if cleanupRBAC && extension.Spec.ServiceAccount.Name != "" {
		serviceAccountName := extension.Spec.ServiceAccount.Name
		clusterRoleBindingName := fmt.Sprintf("opconsh-%s-cluster-admin", extensionName)
		fmt.Printf("• ServiceAccount '%s' in namespace '%s'\n", serviceAccountName, extension.Spec.Namespace)
		fmt.Printf("• ClusterRoleBinding '%s'\n", clusterRoleBindingName)
		
		if cleanupNamespace {
			fmt.Printf("• Namespace '%s' (if empty and created by opconsh)\n", extension.Spec.Namespace)
		}
	}
	fmt.Println()

	// Confirmation prompt
	if !skipConfirmation {
		fmt.Print("Continue with uninstall? (type 'yes' to confirm): ")
		var response string
		fmt.Scanln(&response)
		if response != "yes" {
			fmt.Println("Uninstall cancelled.")
			return nil
		}
	}

	// Perform uninstall
	fmt.Println("Removing resources...")

	// Delete the ClusterExtension first
	fmt.Printf("[+] Removing ClusterExtension '%s'...\n", extensionName)
	if err := r.olmClient.DeleteClusterExtension(r.ctx, extensionName); err != nil {
		return fmt.Errorf("failed to delete ClusterExtension: %w", err)
	}

	if cleanupRBAC && extension.Spec.ServiceAccount.Name != "" {
		serviceAccountName := extension.Spec.ServiceAccount.Name
		namespace := extension.Spec.Namespace
		clusterRoleBindingName := fmt.Sprintf("opconsh-%s-cluster-admin", extensionName)

		// Delete ClusterRoleBinding
		fmt.Printf("[+] Removing ClusterRoleBinding '%s'...\n", clusterRoleBindingName)
		if err := r.olmClient.DeleteClusterRoleBinding(r.ctx, clusterRoleBindingName); err != nil {
			fmt.Printf("Warning: Failed to delete ClusterRoleBinding: %v\n", err)
		}

		// Delete ServiceAccount
		fmt.Printf("[+] Removing ServiceAccount '%s'...\n", serviceAccountName)
		if err := r.olmClient.DeleteServiceAccount(r.ctx, namespace, serviceAccountName); err != nil {
			fmt.Printf("Warning: Failed to delete ServiceAccount: %v\n", err)
		}

		// Optionally delete namespace if empty
		if cleanupNamespace && namespace != "default" {
			fmt.Printf("[+] Checking if namespace '%s' can be removed...\n", namespace)
			if err := r.olmClient.DeleteNamespaceIfEmpty(r.ctx, namespace); err != nil {
				fmt.Printf("Note: Namespace '%s' not removed: %v\n", namespace, err)
			} else {
				fmt.Printf("[+] Namespace '%s' removed\n", namespace)
			}
		}
	}

	fmt.Println()
	fmt.Println("✅ Experimental extension uninstall completed!")
	fmt.Println()

	if cleanupRBAC {
		fmt.Printf("Use 'extensions list' to verify the extension was removed\n")
	} else {
		fmt.Printf("Note: RBAC resources were kept as requested\n")
		fmt.Printf("Use 'extensions list' to verify the extension was removed\n")
	}

	return nil
}

// showUninstallHelp displays help for the uninstall-experimental command
func (r *REPL) showUninstallHelp() error {
	fmt.Println("uninstall-experimental - Remove a ClusterExtension and cleanup associated resources")
	fmt.Println()
	fmt.Println("⚠️  WARNING: This command permanently removes extensions!")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  extensions uninstall-experimental <extension-name> [options]")
	fmt.Println("  ext uninstall-experimental <extension-name> [options]")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  ext uninstall-experimental prometheus-operator")
	fmt.Println("  ext uninstall-experimental grafana-operator --keep-namespace")
	fmt.Println("  ext uninstall-experimental my-extension --keep-rbac --yes")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  --keep-rbac         Keep ServiceAccount and ClusterRoleBinding (default: remove)")
	fmt.Println("  --keep-namespace    Keep namespace even if empty (default: remove if empty)")
	fmt.Println("  --yes               Skip confirmation prompt")
	fmt.Println("  --help, -h, help    Show this help message")
	fmt.Println()
	fmt.Println("What gets removed:")
	fmt.Println("• ClusterExtension resource (always removed)")
	fmt.Println("• ServiceAccount created by opconsh (unless --keep-rbac)")
	fmt.Println("• ClusterRoleBinding created by opconsh (unless --keep-rbac)")
	fmt.Println("• Namespace if empty and created by opconsh (unless --keep-namespace)")
	fmt.Println()
	fmt.Println("Safety Features:")
	fmt.Println("• Only removes resources with 'created-by: opconsh' labels")
	fmt.Println("• Won't delete namespaces that contain other resources")
	fmt.Println("• Requires confirmation unless --yes is used")
	fmt.Println()
	return nil
}
