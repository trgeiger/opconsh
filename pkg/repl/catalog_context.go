package repl

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/chzyer/readline"
	"github.com/operator-framework/opconsh/pkg/client"
	"github.com/operator-framework/opconsh/pkg/portforward"
	olmv1 "github.com/operator-framework/operator-controller/api/v1"
	"golang.org/x/term"
)

// CatalogContext represents an interactive context for a specific catalog
type CatalogContext struct {
	repl           *REPL
	catalog        *olmv1.ClusterCatalog
	portForward    *portforward.PortForwarder
	packages       []client.Package
	packagesLoaded bool
}

// EnterCatalogContext enters an interactive context for a specific catalog
func (r *REPL) EnterCatalogContext(catalogName string) error {
	// Get the catalog
	catalog, err := r.olmClient.GetClusterCatalog(r.ctx, catalogName)
	if err != nil {
		return fmt.Errorf("failed to get catalog '%s': %w", catalogName, err)
	}

	if catalog.Status.URLs == nil {
		return fmt.Errorf("catalog '%s' has no status URLs - catalog may not be ready", catalogName)
	}

	fmt.Printf("Entering catalog context for '%s'...\n", catalogName)
	fmt.Println("Setting up port forward to catalogd service...")

	// Set up port forwarding
	pf := portforward.NewPortForwarder(r.config, r.k8sClient)
	if err := pf.Start(); err != nil {
		return fmt.Errorf("failed to start port forwarding: %w", err)
	}

	fmt.Printf("Port forward established on %s\n", pf.GetLocalURL())
	fmt.Printf("Entered catalog '%s' - type 'help' for catalog commands or 'exit' to return\n", catalogName)
	fmt.Println()

	// Create catalog context
	ctx := &CatalogContext{
		repl:        r,
		catalog:     catalog,
		portForward: pf,
	}

	// Pre-load packages for better UX and autocomplete performance
	if err := ctx.ensurePackagesLoaded(); err != nil {
		fmt.Printf("Warning: Failed to load packages: %v\n", err)
		fmt.Println("Problem connecting to the catalogd endpoint, catalog interactions may not function properly")
	} else {
		fmt.Printf("Loaded %d packages\n", len(ctx.packages))
	}
	fmt.Println()

	// Start catalog REPL
	err = ctx.Start()

	// Clean up port forwarding when exiting
	fmt.Println("Stopping port forward...")
	pf.Stop()
	fmt.Printf("Exited catalog '%s'\n", catalogName)

	return err
}

// Start begins the catalog-specific REPL session
func (ctx *CatalogContext) Start() error {
	// Set up readline for catalog context
	completer := ctx.setupCatalogCompletion()
	rl, err := readline.NewEx(&readline.Config{
		Prompt:                 fmt.Sprintf("%s> ", ctx.catalog.Name),
		AutoComplete:           completer,
		HistoryFile:            fmt.Sprintf("/tmp/.opconsh_catalog_%s_history", ctx.catalog.Name),
		HistoryLimit:           1000,
		DisableAutoSaveHistory: false,
		VimMode:                false,
	})
	if err != nil {
		return fmt.Errorf("failed to create readline: %w", err)
	}
	defer rl.Close()

	for {
		input, err := rl.Readline()
		if err != nil {
			return nil // EOF or interrupt
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		if input == "exit" || input == "quit" || input == ".." {
			return nil
		}

		if err := ctx.processCommand(input); err != nil {
			fmt.Printf("Error: %v\n", err)
		}
	}
}

// processCommand handles catalog context commands
func (ctx *CatalogContext) processCommand(input string) error {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return nil
	}

	command := parts[0]
	args := parts[1:]

	switch command {
	case "help", "h":
		return ctx.showCatalogHelp()
	case "packages", "list", "ls":
		return ctx.listPackages()
	case "package", "get":
		if len(args) < 1 {
			return fmt.Errorf("'package' requires a package name")
		}
		return ctx.getPackage(args[0])
	case "search":
		if len(args) < 1 {
			return fmt.Errorf("'search' requires a search term")
		}
		return ctx.searchPackages(args[0])
	case "describe", "desc":
		if len(args) < 1 {
			return fmt.Errorf("'describe' requires a package name")
		}
		return ctx.describePackage(args[0])
	case "info":
		return ctx.showCatalogInfo()
	case "refresh":
		return ctx.refreshPackages()
	case "clear":
		return ctx.clearScreen()
	case "channels":
		if len(args) < 1 {
			return fmt.Errorf("'channels' requires a package name")
		}
		return ctx.listChannels(args[0])
	case "bundles":
		if len(args) < 1 {
			return fmt.Errorf("'bundles' requires a package name")
		}
		channel := ""
		if len(args) > 1 {
			channel = args[1]
		}
		return ctx.listBundles(args[0], channel)
	case "versions":
		if len(args) < 1 {
			return fmt.Errorf("'versions' requires a package name")
		}
		channel := ""
		if len(args) > 1 {
			channel = args[1]
		}
		return ctx.listVersions(args[0], channel)
	case "install-experimental":
		// Check for help first
		if len(args) >= 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
			return ctx.repl.showInstallHelp()
		}
		if len(args) < 1 {
			fmt.Println("Error: 'install-experimental' requires a package name")
			fmt.Println()
			return ctx.repl.showInstallHelp()
		}
		return ctx.installPackageExperimental(args[0], args[1:])
	case "uninstall-experimental":
		// Check for help first
		if len(args) >= 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
			return ctx.repl.showUninstallHelp()
		}
		if len(args) < 1 {
			fmt.Println("Error: 'uninstall-experimental' requires an extension name")
			fmt.Println()
			return ctx.repl.showUninstallHelp()
		}
		return ctx.uninstallExtensionExperimental(args[0], args[1:])
	default:
		return fmt.Errorf("unknown command: %s. Type 'help' for available commands", command)
	}
}

// showCatalogHelp displays available commands in catalog context
func (ctx *CatalogContext) showCatalogHelp() error {
	fmt.Printf("Catalog '%s' commands:\n", ctx.catalog.Name)
	fmt.Println()
	fmt.Println("  help, h                    Show this help message")
	fmt.Println("  packages, list, ls         List all packages in this catalog")
	fmt.Println("  package <name>, get <name> Get detailed package information")
	fmt.Println("  search <term>              Search packages by name or description")
	fmt.Println("  describe <name>, desc <name> View full package description in pager")
	fmt.Println()
	fmt.Println("  Package details:")
	fmt.Println("  channels <name>            List all channels for a package")
	fmt.Println("  bundles <name> [channel]   List all bundles for a package (or specific channel)")
	fmt.Println("  versions <name> [channel]  List all versions for a package (or specific channel)")
	fmt.Println()
	fmt.Println("  Installation:")
	fmt.Println("  install-experimental <pkg>   [⚠️ TESTING ONLY] Install extension with cluster-admin SA")
	fmt.Println("  uninstall-experimental <ext> [⚠️ TESTING ONLY] Uninstall extension and cleanup RBAC")
	fmt.Println()
	fmt.Println("  Other commands:")
	fmt.Println("  info                       Show catalog information")
	fmt.Println("  refresh                    Refresh package list")
	fmt.Println("  clear                      Clear the screen")
	fmt.Println("  exit, quit, ..             Exit catalog context")
	fmt.Println()
	return nil
}

// showCatalogInfo displays information about the current catalog
func (ctx *CatalogContext) showCatalogInfo() error {
	fmt.Printf("Catalog: %s\n\n", ctx.catalog.Name)
	fmt.Printf("Source Type:     %s\n", ctx.catalog.Spec.Source.Type)
	if ctx.catalog.Spec.Source.Image != nil {
		fmt.Printf("Source Image:    %s\n", ctx.catalog.Spec.Source.Image.Ref)
	}
	fmt.Printf("Created:         %s\n", ctx.catalog.CreationTimestamp.Format("2006-01-02 15:04:05"))
	fmt.Printf("Base URL:        %s\n", ctx.catalog.Status.URLs.Base)

	if len(ctx.catalog.Status.Conditions) > 0 {
		fmt.Printf("\nConditions:\n")
		for _, condition := range ctx.catalog.Status.Conditions {
			fmt.Printf("  %s: %s (%s)\n", condition.Type, condition.Status, condition.Reason)
			if condition.Message != "" {
				fmt.Printf("    Message: %s\n", condition.Message)
			}
		}
	}

	// Show package count if loaded
	if ctx.packagesLoaded {
		fmt.Printf("\nPackages:        %d\n", len(ctx.packages))
	}

	return nil
}

// ensurePackagesLoaded loads packages if not already loaded
func (ctx *CatalogContext) ensurePackagesLoaded() error {
	if ctx.packagesLoaded {
		return nil
	}

	fmt.Println("Loading packages...")

	// Use the port forward URL with the correct catalog path format
	localURL := ctx.portForward.GetLocalURL()
	catalogURL := localURL + "/catalogs/" + ctx.catalog.Name
	packages, err := ctx.repl.catalogdClient.GetPackages(ctx.repl.ctx, ctx.catalog.Name, catalogURL)
	if err != nil {
		return fmt.Errorf("failed to load packages: %w", err)
	}

	ctx.packages = packages
	ctx.packagesLoaded = true
	return nil
}

// refreshPackages forces a reload of packages
func (ctx *CatalogContext) refreshPackages() error {
	ctx.packagesLoaded = false
	if err := ctx.ensurePackagesLoaded(); err != nil {
		return err
	}
	fmt.Printf("Refreshed %d packages\n", len(ctx.packages))
	return nil
}

// clearScreen clears the terminal screen
func (ctx *CatalogContext) clearScreen() error {
	// ANSI escape sequence to clear screen and move cursor to top-left
	fmt.Print("\033[2J\033[H")
	return nil
}

func (ctx *CatalogContext) listPackages() error {
	if err := ctx.ensurePackagesLoaded(); err != nil {
		return err
	}

	if len(ctx.packages) == 0 {
		fmt.Println("No packages found in this catalog")
		return nil
	}

	// 1. Build the table in a strings.Builder
	var buf strings.Builder
	// Use a clean tabwriter setup
	w := tabwriter.NewWriter(&buf, 0, 8, 2, ' ', 0)

	fmt.Fprintf(&buf, "Found %d package(s):\n\n", len(ctx.packages))
	fmt.Fprintln(w, "NAME\tDEFAULT CHANNEL\tCHANNELS")
	fmt.Fprintln(w, "----\t---------------\t--------")

	for _, pkg := range ctx.packages {
		fmt.Fprintf(w, "%s\t%s\t%d\n", pkg.Name, pkg.DefaultChannel, len(pkg.Channels))
	}
	w.Flush()

	output := buf.String()
	return printOrPager(output)

}

func printOrPager(output string) error {
	// 2. Determine if we should page
	_, termHeight, err := term.GetSize(int(os.Stdout.Fd()))
	lineCount := strings.Count(output, "\n")

	// If we can't get terminal size or it's short enough, just print it
	if err != nil || lineCount < termHeight-2 {
		fmt.Print(output)
		return nil
	}

	// 3. Otherwise, send to system pager
	return smartPage(output)
}

func smartPage(content string) error {
	// -R: Handle ANSI colors (if you add them later)
	// -F: Quit if the content fits on one screen (double safety)
	// -X: Don't clear screen on exit
	pager := os.Getenv("PAGER")
	if pager == "" {
		pager = "less"
	}

	cmd := exec.Command(pager, "-R", "-F", "-X")
	cmd.Stdin = strings.NewReader(content)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// getPackage displays detailed information about a specific package
func (ctx *CatalogContext) getPackage(packageName string) error {
	if err := ctx.ensurePackagesLoaded(); err != nil {
		return err
	}

	var targetPkg *client.Package
	for _, pkg := range ctx.packages {
		if pkg.Name == packageName {
			targetPkg = &pkg
			break
		}
	}

	if targetPkg == nil {
		return fmt.Errorf("package '%s' not found in catalog '%s'", packageName, ctx.catalog.Name)
	}

	fmt.Printf("Package: %s\n\n", targetPkg.Name)
	fmt.Printf("Default Channel: %s\n", targetPkg.DefaultChannel)

	if len(targetPkg.Channels) > 0 {
		fmt.Printf("\nChannels (%d):\n", len(targetPkg.Channels))
		for _, channel := range targetPkg.Channels {
			fmt.Printf("  %s (%d bundles)\n", channel.Name, len(channel.Entries))
			if len(channel.Entries) > 0 {
				// Show the latest bundle
				latest := channel.Entries[0]
				if latest.Version != "" {
					fmt.Printf("    Latest: %s (version %s)\n", latest.Name, latest.Version)
				} else {
					fmt.Printf("    Latest: %s\n", latest.Name)
				}
			}
		}
	}

	return nil
}

// searchPackages searches for packages by name
func (ctx *CatalogContext) searchPackages(searchTerm string) error {
	if err := ctx.ensurePackagesLoaded(); err != nil {
		return err
	}

	var matches []client.Package
	searchLower := strings.ToLower(searchTerm)

	for _, pkg := range ctx.packages {
		if strings.Contains(strings.ToLower(pkg.Name), searchLower) {
			matches = append(matches, pkg)
		}
	}

	if len(matches) == 0 {
		fmt.Printf("No packages found matching '%s'\n", searchTerm)
		return nil
	}

	fmt.Printf("Found %d matching package(s):\n\n", len(matches))
	fmt.Printf("%-50s %-20s %-8s\n", "NAME", "DEFAULT CHANNEL", "CHANNELS")
	fmt.Println(strings.Repeat("-", 82))

	for _, pkg := range matches {
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

// setupCatalogCompletion configures tab completion for catalog context
func (ctx *CatalogContext) setupCatalogCompletion() *readline.PrefixCompleter {
	return readline.NewPrefixCompleter(
		readline.PcItem("help"),
		readline.PcItem("h"),
		readline.PcItem("packages"),
		readline.PcItem("list"),
		readline.PcItem("ls"),
		readline.PcItem("package",
			readline.PcItemDynamic(ctx.packageNamesCompleter),
		),
		readline.PcItem("get",
			readline.PcItemDynamic(ctx.packageNamesCompleter),
		),
		readline.PcItem("search"),
		readline.PcItem("describe",
			readline.PcItemDynamic(ctx.packageNamesCompleter),
		),
		readline.PcItem("desc",
			readline.PcItemDynamic(ctx.packageNamesCompleter),
		),
		readline.PcItem("channels",
			readline.PcItemDynamic(ctx.packageNamesCompleter),
		),
		readline.PcItem("versions",
			readline.PcItemDynamic(ctx.packageNamesCompleter),
		),
		readline.PcItem("bundles",
			readline.PcItemDynamic(ctx.packageNamesCompleter),
		),
		readline.PcItem("install-experimental",
			readline.PcItemDynamic(ctx.packageNamesCompleter),
			readline.PcItem("--help"),
			readline.PcItem("-h"),
			readline.PcItem("help"),
			readline.PcItem("--namespace"),
			readline.PcItem("--name"),
			readline.PcItem("--version"),
			readline.PcItem("--channel"),
			readline.PcItem("--yes"),
		),
		readline.PcItem("uninstall-experimental",
			readline.PcItemDynamic(ctx.extensionNamesCompleter),
			readline.PcItem("--help"),
			readline.PcItem("-h"),
			readline.PcItem("help"),
			readline.PcItem("--keep-rbac"),
			readline.PcItem("--keep-namespace"),
			readline.PcItem("--yes"),
		),
		readline.PcItem("info"),
		readline.PcItem("refresh"),
		readline.PcItem("clear"),
		readline.PcItem("exit"),
		readline.PcItem("quit"),
		readline.PcItem(".."),
	)
}

// packageNamesCompleter provides tab completion for package names in this catalog
func (ctx *CatalogContext) packageNamesCompleter(line string) []string {
	if err := ctx.ensurePackagesLoaded(); err != nil {
		return nil
	}

	var names []string
	for _, pkg := range ctx.packages {
		names = append(names, pkg.Name)
	}
	return names
}

// extensionNamesCompleter provides tab completion for extension names
func (ctx *CatalogContext) extensionNamesCompleter(line string) []string {
	// Use the main REPL's extension completer
	return ctx.repl.extensionNamesCompleter(line)
}

// getChannelNames returns channel names for a specific package
func (ctx *CatalogContext) getChannelNames(packageName string) []string {
	for _, pkg := range ctx.packages {
		if pkg.Name == packageName {
			var channels []string
			for _, channel := range pkg.Channels {
				channels = append(channels, channel.Name)
			}
			return channels
		}
	}
	return nil
}

// getVersions returns all available versions for a specific package
func (ctx *CatalogContext) getVersions(packageName string) []string {
	for _, pkg := range ctx.packages {
		if pkg.Name == packageName {
			versionSet := make(map[string]bool)
			for _, channel := range pkg.Channels {
				for _, bundle := range channel.Entries {
					if bundle.Version != "" {
						versionSet[bundle.Version] = true
					}
				}
			}

			var versions []string
			for version := range versionSet {
				versions = append(versions, version)
			}

			// Sort versions using the same logic as listVersions
			sort.Slice(versions, func(i, j int) bool {
				vi := parseSemanticVersion(versions[i])
				vj := parseSemanticVersion(versions[j])

				if vi != nil && vj != nil {
					return compareSemanticVersions(vi, vj) < 0
				}

				return versions[i] < versions[j]
			})

			return versions
		}
	}
	return nil
}

// describePackage shows the full description of a package in a pager
func (ctx *CatalogContext) describePackage(packageName string) error {
	if err := ctx.ensurePackagesLoaded(); err != nil {
		return fmt.Errorf("failed to load packages: %w", err)
	}

	// Find the package
	var targetPkg *client.Package
	for _, pkg := range ctx.packages {
		if pkg.Name == packageName {
			targetPkg = &pkg
			break
		}
	}

	if targetPkg == nil {
		return fmt.Errorf("package '%s' not found in catalog '%s'", packageName, ctx.catalog.Name)
	}

	// Get the best description from all bundles across all channels
	description, _ := client.FindBestDescription(targetPkg)
	RunMarkdownPager(description)

	return nil
}

// showInPager displays content using the system pager (less/more)
func showInPager(content string) error {
	// Try to use less first, fall back to more, then just print
	pagers := []string{"less", "more"}

	for _, pager := range pagers {
		if _, err := exec.LookPath(pager); err == nil {
			cmd := exec.Command(pager)
			cmd.Stdin = strings.NewReader(content)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			return cmd.Run()
		}
	}

	// If no pager is available, just print the content
	fmt.Print(content)
	return nil
}

// listChannels shows all available channels for a package
func (ctx *CatalogContext) listChannels(packageName string) error {
	if err := ctx.ensurePackagesLoaded(); err != nil {
		return fmt.Errorf("failed to load packages: %w", err)
	}

	// Find the package
	var targetPkg *client.Package
	for _, pkg := range ctx.packages {
		if pkg.Name == packageName {
			targetPkg = &pkg
			break
		}
	}

	if targetPkg == nil {
		return fmt.Errorf("package '%s' not found in catalog '%s'", packageName, ctx.catalog.Name)
	}

	fmt.Printf("Channels for package '%s':\n\n", packageName)

	for _, channel := range targetPkg.Channels {
		isDefault := ""
		if channel.Name == targetPkg.DefaultChannel {
			isDefault = " (default)"
		}

		fmt.Printf("  %-20s %d bundles%s\n", channel.Name, len(channel.Entries), isDefault)
	}

	fmt.Printf("\nTotal channels: %d\n", len(targetPkg.Channels))
	return nil
}

// listBundles shows all bundles for a package, optionally filtered by channel
func (ctx *CatalogContext) listBundles(packageName, channelFilter string) error {
	if err := ctx.ensurePackagesLoaded(); err != nil {
		return fmt.Errorf("failed to load packages: %w", err)
	}

	// Find the package
	var targetPkg *client.Package
	for _, pkg := range ctx.packages {
		if pkg.Name == packageName {
			targetPkg = &pkg
			break
		}
	}

	if targetPkg == nil {
		return fmt.Errorf("package '%s' not found in catalog '%s'", packageName, ctx.catalog.Name)
	}

	if channelFilter != "" {
		fmt.Printf("Bundles for package '%s' in channel '%s':\n\n", packageName, channelFilter)
	} else {
		fmt.Printf("Bundles for package '%s' (all channels):\n\n", packageName)
	}

	totalBundles := 0
	for _, channel := range targetPkg.Channels {
		if channelFilter != "" && channel.Name != channelFilter {
			continue
		}

		if channelFilter == "" {
			fmt.Printf("Channel: %s\n", channel.Name)
		}

		for _, bundle := range channel.Entries {
			fmt.Printf("  %-30s %s\n", bundle.Name, bundle.Version)
			totalBundles++
		}

		if channelFilter == "" && len(channel.Entries) > 0 {
			fmt.Println()
		}
	}

	if totalBundles == 0 {
		if channelFilter != "" {
			return fmt.Errorf("no bundles found in channel '%s' for package '%s'", channelFilter, packageName)
		} else {
			return fmt.Errorf("no bundles found for package '%s'", packageName)
		}
	}

	fmt.Printf("Total bundles: %d\n", totalBundles)
	return nil
}

// listVersions shows all versions for a package, optionally filtered by channel
func (ctx *CatalogContext) listVersions(packageName, channelFilter string) error {
	if err := ctx.ensurePackagesLoaded(); err != nil {
		return fmt.Errorf("failed to load packages: %w", err)
	}

	// Find the package
	var targetPkg *client.Package
	for _, pkg := range ctx.packages {
		if pkg.Name == packageName {
			targetPkg = &pkg
			break
		}
	}

	if targetPkg == nil {
		return fmt.Errorf("package '%s' not found in catalog '%s'", packageName, ctx.catalog.Name)
	}

	if channelFilter != "" {
		fmt.Printf("Versions for package '%s' in channel '%s':\n\n", packageName, channelFilter)
	} else {
		fmt.Printf("Versions for package '%s' (all channels):\n\n", packageName)
	}

	versions := make(map[string][]string) // version -> channels
	for _, channel := range targetPkg.Channels {
		if channelFilter != "" && channel.Name != channelFilter {
			continue
		}

		for _, bundle := range channel.Entries {
			if versions[bundle.Version] == nil {
				versions[bundle.Version] = make([]string, 0)
			}
			// Check if channel name already exists for this version
			channelExists := false
			for _, existingChannel := range versions[bundle.Version] {
				if existingChannel == channel.Name {
					channelExists = true
					break
				}
			}
			if !channelExists {
				versions[bundle.Version] = append(versions[bundle.Version], channel.Name)
			}
		}
	}

	if len(versions) == 0 {
		if channelFilter != "" {
			return fmt.Errorf("no versions found in channel '%s' for package '%s'", channelFilter, packageName)
		} else {
			return fmt.Errorf("no versions found for package '%s'", packageName)
		}
	}

	// Sort versions for display
	sortedVersions := make([]string, 0, len(versions))
	for version := range versions {
		sortedVersions = append(sortedVersions, version)
	}

	// Sort versions using semantic version comparison where possible
	sort.Slice(sortedVersions, func(i, j int) bool {
		// Handle empty versions first
		if sortedVersions[i] == "" {
			return false
		}
		if sortedVersions[j] == "" {
			return true
		}

		// Try semantic version parsing first
		vi := parseSemanticVersion(sortedVersions[i])
		vj := parseSemanticVersion(sortedVersions[j])

		if vi != nil && vj != nil {
			return compareSemanticVersions(vi, vj) < 0
		}

		// Fall back to string comparison
		return sortedVersions[i] < sortedVersions[j]
	})

	for _, version := range sortedVersions {
		channels := versions[version]
		if version == "" {
			fmt.Printf("  %-20s (channels: %s)\n", "<no version>", strings.Join(channels, ", "))
		} else {
			fmt.Printf("  %-20s (channels: %s)\n", version, strings.Join(channels, ", "))
		}
	}

	fmt.Printf("\nTotal versions: %d\n", len(versions))
	return nil
}

// showBundle shows detailed information about a specific bundle
func (ctx *CatalogContext) showBundle(packageName, version string) error {
	if err := ctx.ensurePackagesLoaded(); err != nil {
		return fmt.Errorf("failed to load packages: %w", err)
	}

	// Find the package
	var targetPkg *client.Package
	for _, pkg := range ctx.packages {
		if pkg.Name == packageName {
			targetPkg = &pkg
			break
		}
	}

	if targetPkg == nil {
		return fmt.Errorf("package '%s' not found in catalog '%s'", packageName, ctx.catalog.Name)
	}

	// Find the bundle with the specified version
	var targetBundle *client.Bundle
	var bundleChannel string
	for _, channel := range targetPkg.Channels {
		for _, bundle := range channel.Entries {
			if bundle.Version == version {
				targetBundle = &bundle
				bundleChannel = channel.Name
				break
			}
		}
		if targetBundle != nil {
			break
		}
	}

	if targetBundle == nil {
		return fmt.Errorf("bundle with version '%s' not found for package '%s'", version, packageName)
	}

	// Create detailed bundle information
	content := fmt.Sprintf("Bundle Information\n")
	content += strings.Repeat("=", 50) + "\n\n"
	content += fmt.Sprintf("Package: %s\n", packageName)
	content += fmt.Sprintf("Bundle: %s\n", targetBundle.Name)
	content += fmt.Sprintf("Version: %s\n", targetBundle.Version)
	content += fmt.Sprintf("Channel: %s\n", bundleChannel)
	content += fmt.Sprintf("Image: %s\n", targetBundle.Image)

	if targetBundle.DisplayName != "" {
		content += fmt.Sprintf("Display Name: %s\n", targetBundle.DisplayName)
	}

	content += "\n"

	if targetBundle.Description != "" {
		content += "Description:\n"
		content += strings.Repeat("-", 30) + "\n"
		content += targetBundle.Description + "\n\n"
	}

	if len(targetBundle.RelatedImages) > 0 {
		content += "Related Images:\n"
		content += strings.Repeat("-", 30) + "\n"
		for _, img := range targetBundle.RelatedImages {
			if img.Name != "" {
				content += fmt.Sprintf("  %s: %s\n", img.Name, img.Image)
			} else {
				content += fmt.Sprintf("  %s\n", img.Image)
			}
		}
		content += "\n"
	}

	// Display in pager for long content, or just print for shorter content
	if len(content) > 2000 {
		return showInPager(content)
	} else {
		fmt.Print(content)
		return nil
	}
}

// semanticVersion represents a semantic version with major, minor, patch components
type semanticVersion struct {
	major int
	minor int
	patch int
}

// parseSemanticVersion attempts to parse a version string into semantic version components
func parseSemanticVersion(version string) *semanticVersion {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return nil
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil
	}

	patch := 0
	if len(parts) >= 3 {
		patch, err = strconv.Atoi(parts[2])
		if err != nil {
			return nil
		}
	}

	return &semanticVersion{major: major, minor: minor, patch: patch}
}

// compareSemanticVersions compares two semantic versions
// Returns -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2
func compareSemanticVersions(v1, v2 *semanticVersion) int {
	if v1.major != v2.major {
		if v1.major < v2.major {
			return -1
		}
		return 1
	}

	if v1.minor != v2.minor {
		if v1.minor < v2.minor {
			return -1
		}
		return 1
	}

	if v1.patch != v2.patch {
		if v1.patch < v2.patch {
			return -1
		}
		return 1
	}

	return 0
}

// installPackageExperimental installs a ClusterExtension with cluster-admin ServiceAccount from within catalog context
func (ctx *CatalogContext) installPackageExperimental(packageName string, options []string) error {
	// Call the main REPL's install method with the current catalog name
	return ctx.repl.installExtensionExperimental(ctx.catalog.Name, packageName, options)
}

// uninstallExtensionExperimental uninstalls a ClusterExtension from within catalog context
func (ctx *CatalogContext) uninstallExtensionExperimental(extensionName string, options []string) error {
	// Call the main REPL's uninstall method
	return ctx.repl.uninstallExtensionExperimental(extensionName, options)
}
