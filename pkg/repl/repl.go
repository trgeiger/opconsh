package repl

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/chzyer/readline"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/operator-framework/opconsh/pkg/client"
	"github.com/operator-framework/opconsh/pkg/portforward"
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
		fmt.Printf("  ✗ Kubeconfig: Unable to read config (%v)\n", err)
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

		fmt.Printf("  ✓ Kubeconfig: %s (context: %s)\n", kubeconfigPath, currentContext)

		// Show current namespace if set
		namespace, _, err := r.kubeConfig.Namespace()
		if err == nil && namespace != "" {
			fmt.Printf("  ✓ Namespace: %s\n", namespace)
		}
	}

	// Check API server connectivity and version
	version, err := r.k8sClient.Discovery().ServerVersion()
	if err != nil {
		fmt.Printf("  ✗ API Server: Failed to connect (%v)\n", err)
		return err
	}

	fmt.Printf("  ✓ API Server: %s (Kubernetes %s)\n", r.config.Host, version.GitVersion)

	// Test user permissions by trying to list namespaces
	namespaces, err := r.k8sClient.CoreV1().Namespaces().List(r.ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		fmt.Printf("  ⚠ Permissions: Limited access (%v)\n", err)
	} else {
		fmt.Printf("  ✓ Permissions: Can list cluster resources (%d namespaces)\n", len(namespaces.Items))
	}

	fmt.Println()
	fmt.Println("OLM Status:")

	// Check catalogd availability
	catalogdNamespace, err := r.findCatalogdNamespace()
	if err != nil {
		fmt.Printf("  ✗ Catalogd: Not found (%v)\n", err)
	} else {
		// Check catalogd pods
		pods, err := r.k8sClient.CoreV1().Pods(catalogdNamespace).List(r.ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/name=catalogd",
		})
		if err != nil {
			fmt.Printf("  ✗ Catalogd: Error checking pods in %s (%v)\n", catalogdNamespace, err)
		} else if len(pods.Items) == 0 {
			fmt.Printf("  ✗ Catalogd: No pods found in %s\n", catalogdNamespace)
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
				fmt.Printf("  ⚠ Catalogd: %d pod(s) in %s, none ready\n", len(pods.Items), catalogdNamespace)
			} else {
				fmt.Printf("  ✓ Catalogd: %d/%d pod(s) ready in %s\n", readyPods, len(pods.Items), catalogdNamespace)
			}
		}
	}

	// Check operator-controller availability
	operatorControllerNamespace, err := r.findOperatorControllerNamespace()
	if err != nil {
		fmt.Printf("  ✗ Operator Controller: Not found (%v)\n", err)
	} else {
		// Check operator-controller pods
		pods, err := r.k8sClient.CoreV1().Pods(operatorControllerNamespace).List(r.ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/name=operator-controller",
		})
		if err != nil {
			fmt.Printf("  ✗ Operator Controller: Error checking pods in %s (%v)\n", operatorControllerNamespace, err)
		} else if len(pods.Items) == 0 {
			fmt.Printf("  ✗ Operator Controller: No pods found in %s\n", operatorControllerNamespace)
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
				fmt.Printf("  ⚠ Operator Controller: %d pod(s) in %s, none ready\n", len(pods.Items), operatorControllerNamespace)
			} else {
				fmt.Printf("  ✓ Operator Controller: %d/%d pod(s) ready in %s\n", readyPods, len(pods.Items), operatorControllerNamespace)
			}
		}
	}

	// Check ClusterCatalogs with detailed error reporting
	catalogs, err := r.olmClient.GetClusterCatalogs(r.ctx)
	if err != nil {
		fmt.Printf("  ✗ ClusterCatalogs: Unable to access (%v)\n", err)
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
			fmt.Printf("  ⚠ ClusterCatalogs: %d available, %d with errors\n", availableCount, errorCount)
		} else {
			fmt.Printf("  ✓ ClusterCatalogs: %d available\n", len(catalogs))
		}
	}

	// Check ClusterExtensions with detailed status
	extensions, err := r.olmClient.GetClusterExtensions(r.ctx)
	if err != nil {
		fmt.Printf("  ✗ ClusterExtensions: Unable to access (%v)\n", err)
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
			fmt.Printf("  ⚠ ClusterExtensions: %d installed, %d failed\n", installedCount, failedCount)
		} else {
			fmt.Printf("  ✓ ClusterExtensions: %d installed\n", len(extensions))
		}
	}

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
	default:
		return fmt.Errorf("unknown extension command: %s", args[0])
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
		fmt.Printf("\nConditions:\n")
		for _, condition := range catalog.Status.Conditions {
			fmt.Printf("  %s: %s (%s)\n", condition.Type, condition.Status, condition.Reason)
			if condition.Message != "" {
				fmt.Printf("    Message: %s\n", condition.Message)
			}
		}
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
		fmt.Printf("\nConditions:\n")
		for _, condition := range extension.Status.Conditions {
			fmt.Printf("  %s: %s (%s)\n", condition.Type, condition.Status, condition.Reason)
			if condition.Message != "" {
				fmt.Printf("    Message: %s\n", condition.Message)
			}
		}
	}

	return nil
}
