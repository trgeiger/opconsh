package repl

import (
	"strings"

	"github.com/chzyer/readline"
)

// setupCompletion configures tab completion for the REPL
func (r *REPL) setupCompletion() *readline.PrefixCompleter {
	return readline.NewPrefixCompleter(
		readline.PcItem("help"),
		readline.PcItem("h"),
		readline.PcItem("status"),
		readline.PcItem("refresh"),
		readline.PcItem("clear"),
		readline.PcItem("enter",
			readline.PcItemDynamic(r.catalogNamesCompleter),
		),
		readline.PcItem("exit"),
		readline.PcItem("quit"),
		readline.PcItem("catalogs",
			readline.PcItem("list"),
			readline.PcItem("ls"),
			readline.PcItemDynamic(r.catalogNamesCompleter,
				readline.PcItem("get"),
				readline.PcItem("packages"),
				readline.PcItem("package",
					readline.PcItemDynamic(r.packageNamesCompleter),
				),
				readline.PcItem("search"),
			),
		),
		readline.PcItem("cc",
			readline.PcItem("list"),
			readline.PcItem("ls"),
			readline.PcItemDynamic(r.catalogNamesCompleter,
				readline.PcItem("get"),
				readline.PcItem("packages"),
				readline.PcItem("package",
					readline.PcItemDynamic(r.packageNamesCompleter),
				),
				readline.PcItem("search"),
			),
		),
		readline.PcItem("extensions",
			readline.PcItem("list"),
			readline.PcItem("ls"),
			readline.PcItem("get",
				readline.PcItemDynamic(r.extensionNamesCompleter),
			),
		),
		readline.PcItem("ext",
			readline.PcItem("list"),
			readline.PcItem("ls"),
			readline.PcItem("get",
				readline.PcItemDynamic(r.extensionNamesCompleter),
			),
		),
	)
}

// catalogNamesCompleter provides tab completion for catalog names
func (r *REPL) catalogNamesCompleter(line string) []string {
	catalogs, err := r.cache.GetCatalogs(r)
	if err != nil {
		return nil
	}

	var names []string
	for _, catalog := range catalogs {
		names = append(names, catalog.Name)
	}
	return names
}

// extensionNamesCompleter provides tab completion for extension names
func (r *REPL) extensionNamesCompleter(line string) []string {
	extensions, err := r.cache.GetExtensions(r)
	if err != nil {
		return nil
	}

	var names []string
	for _, extension := range extensions {
		names = append(names, extension.Name)
	}
	return names
}

// packageNamesCompleter provides tab completion for package names within a catalog
func (r *REPL) packageNamesCompleter(line string) []string {
	// Parse the line to extract the catalog name
	parts := strings.Fields(line)
	if len(parts) < 3 {
		return nil
	}

	catalogName := parts[2] // e.g., "catalogs package <catalogName> <packageName>"

	// Get the catalog to get its base URL
	catalogs, err := r.cache.GetCatalogs(r)
	if err != nil {
		return nil
	}

	var catalogBaseURL string
	for _, catalog := range catalogs {
		if catalog.Name == catalogName {
			if catalog.Status.URLs != nil {
				catalogBaseURL = catalog.Status.URLs.Base
			}
			break
		}
	}

	if catalogBaseURL == "" {
		return nil
	}

	packages, err := r.cache.GetPackages(r, catalogName, catalogBaseURL)
	if err != nil {
		return nil
	}

	var names []string
	for _, pkg := range packages {
		names = append(names, pkg.Name)
	}
	return names
}
