package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/operator-framework/opconsh/pkg/client"
	"github.com/operator-framework/opconsh/pkg/repl"
)

var (
	kubeconfig string
)

var rootCmd = &cobra.Command{
	Use:   "opconsh",
	Short: "Interactive CLI for OLMv1 ClusterCatalogs and ClusterExtensions",
	Long: `opconsh is an interactive CLI tool for managing OLMv1 resources.
It provides an ergonomic way to interact with ClusterCatalogs and ClusterExtensions
without writing repetitive kubectl commands or managing YAML files.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Start the interactive REPL
		if err := startREPL(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start REPL: %v\n", err)
			os.Exit(1)
		}
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "path to the kubeconfig file (uses standard kubectl precedence if not specified)")
}

func startREPL() error {
	// Create Kubernetes client using standard kubeconfig loading rules
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		loadingRules.ExplicitPath = kubeconfig
	}
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return fmt.Errorf("failed to build kubeconfig: %w", err)
	}

	k8sClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Create OLM client
	olmClient, err := client.NewOLMClient(config)
	if err != nil {
		return fmt.Errorf("failed to create OLM client: %w", err)
	}

	// Create catalogd client
	catalogdClient, err := client.NewCatalogdClient(config)
	if err != nil {
		return fmt.Errorf("failed to create catalogd client: %w", err)
	}

	// Start REPL
	r := repl.New(k8sClient, olmClient, catalogdClient, config)
	return r.Start()
}
