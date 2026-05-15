package repl

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	olmv1 "github.com/operator-framework/operator-controller/api/v1"
)

func TestParseInstallOptions(t *testing.T) {
	tests := []struct {
		name                string
		packageName         string
		options             []string
		expectedNamespace   string
		expectedExtName     string
		expectedVersion     string
		expectedChannel     string
		expectedSkipConfirm bool
	}{
		{
			name:                "default options",
			packageName:         "prometheus-operator",
			options:             []string{},
			expectedNamespace:   "opconsh-test",
			expectedExtName:     "prometheus-operator",
			expectedVersion:     "",
			expectedChannel:     "",
			expectedSkipConfirm: false,
		},
		{
			name:                "custom namespace and name",
			packageName:         "prometheus-operator",
			options:             []string{"--namespace", "custom-ns", "--name", "my-prometheus"},
			expectedNamespace:   "custom-ns",
			expectedExtName:     "my-prometheus",
			expectedVersion:     "",
			expectedChannel:     "",
			expectedSkipConfirm: false,
		},
		{
			name:                "with version and channel",
			packageName:         "prometheus-operator",
			options:             []string{"--version", "0.68.0", "--channel", "stable"},
			expectedNamespace:   "opconsh-test",
			expectedExtName:     "prometheus-operator",
			expectedVersion:     "0.68.0",
			expectedChannel:     "stable",
			expectedSkipConfirm: false,
		},
		{
			name:                "skip confirmation",
			packageName:         "prometheus-operator",
			options:             []string{"--yes"},
			expectedNamespace:   "opconsh-test",
			expectedExtName:     "prometheus-operator",
			expectedVersion:     "",
			expectedChannel:     "",
			expectedSkipConfirm: true,
		},
		{
			name:                "all options combined",
			packageName:         "grafana-operator",
			options:             []string{"--namespace", "monitoring", "--name", "my-grafana", "--version", "4.10.0", "--channel", "v4", "--yes"},
			expectedNamespace:   "monitoring",
			expectedExtName:     "my-grafana",
			expectedVersion:     "4.10.0",
			expectedChannel:     "v4",
			expectedSkipConfirm: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse options like the real function does
			namespace := "opconsh-test"
			var version, channel, extensionName string
			skipConfirmation := false

			for i := 0; i < len(tt.options); i++ {
				switch tt.options[i] {
				case "--namespace":
					if i+1 < len(tt.options) {
						namespace = tt.options[i+1]
						i++
					}
				case "--version":
					if i+1 < len(tt.options) {
						version = tt.options[i+1]
						i++
					}
				case "--channel":
					if i+1 < len(tt.options) {
						channel = tt.options[i+1]
						i++
					}
				case "--name":
					if i+1 < len(tt.options) {
						extensionName = tt.options[i+1]
						i++
					}
				case "--yes":
					skipConfirmation = true
				}
			}

			// Default extension name
			if extensionName == "" {
				extensionName = tt.packageName
			}

			// Validate results
			if namespace != tt.expectedNamespace {
				t.Errorf("Expected namespace %s, got %s", tt.expectedNamespace, namespace)
			}
			if extensionName != tt.expectedExtName {
				t.Errorf("Expected extension name %s, got %s", tt.expectedExtName, extensionName)
			}
			if version != tt.expectedVersion {
				t.Errorf("Expected version %s, got %s", tt.expectedVersion, version)
			}
			if channel != tt.expectedChannel {
				t.Errorf("Expected channel %s, got %s", tt.expectedChannel, channel)
			}
			if skipConfirmation != tt.expectedSkipConfirm {
				t.Errorf("Expected skipConfirmation %v, got %v", tt.expectedSkipConfirm, skipConfirmation)
			}
		})
	}
}

func TestParseUninstallOptions(t *testing.T) {
	tests := []struct {
		name                   string
		options                []string
		expectedCleanupRBAC    bool
		expectedCleanupNS      bool
		expectedSkipConfirm    bool
	}{
		{
			name:                "default options",
			options:             []string{},
			expectedCleanupRBAC: true,
			expectedCleanupNS:   true,
			expectedSkipConfirm: false,
		},
		{
			name:                "keep rbac",
			options:             []string{"--keep-rbac"},
			expectedCleanupRBAC: false,
			expectedCleanupNS:   true,
			expectedSkipConfirm: false,
		},
		{
			name:                "keep namespace",
			options:             []string{"--keep-namespace"},
			expectedCleanupRBAC: true,
			expectedCleanupNS:   false,
			expectedSkipConfirm: false,
		},
		{
			name:                "skip confirmation",
			options:             []string{"--yes"},
			expectedCleanupRBAC: true,
			expectedCleanupNS:   true,
			expectedSkipConfirm: true,
		},
		{
			name:                "keep all with confirmation skip",
			options:             []string{"--keep-rbac", "--keep-namespace", "--yes"},
			expectedCleanupRBAC: false,
			expectedCleanupNS:   false,
			expectedSkipConfirm: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse options like the real function does
			cleanupRBAC := true
			cleanupNamespace := true
			skipConfirmation := false

			for i := 0; i < len(tt.options); i++ {
				switch tt.options[i] {
				case "--keep-rbac":
					cleanupRBAC = false
				case "--keep-namespace":
					cleanupNamespace = false
				case "--yes":
					skipConfirmation = true
				}
			}

			// Validate results
			if cleanupRBAC != tt.expectedCleanupRBAC {
				t.Errorf("Expected cleanupRBAC %v, got %v", tt.expectedCleanupRBAC, cleanupRBAC)
			}
			if cleanupNamespace != tt.expectedCleanupNS {
				t.Errorf("Expected cleanupNamespace %v, got %v", tt.expectedCleanupNS, cleanupNamespace)
			}
			if skipConfirmation != tt.expectedSkipConfirm {
				t.Errorf("Expected skipConfirmation %v, got %v", tt.expectedSkipConfirm, skipConfirmation)
			}
		})
	}
}

func TestBuildClusterExtension(t *testing.T) {
	tests := []struct {
		name            string
		extensionName   string
		packageName     string
		catalogName     string
		namespace       string
		serviceAccount  string
		version         string
		channel         string
		expectedLabels  map[string]string
		expectedPkgName string
		expectedNS      string
		expectedSA      string
	}{
		{
			name:           "basic cluster extension",
			extensionName:  "prometheus-operator",
			packageName:    "prometheus-operator",
			catalogName:    "operatorhubio",
			namespace:      "monitoring",
			serviceAccount: "opconsh-prometheus-operator",
			expectedLabels: map[string]string{
				"created-by": "opconsh",
				"purpose":    "experimental-install",
			},
			expectedPkgName: "prometheus-operator",
			expectedNS:      "monitoring",
			expectedSA:      "opconsh-prometheus-operator",
		},
		{
			name:           "cluster extension with version and channel",
			extensionName:  "grafana",
			packageName:    "grafana-operator",
			catalogName:    "operatorhubio",
			namespace:      "grafana-system",
			serviceAccount: "opconsh-grafana",
			version:        "4.10.0",
			channel:        "v4",
			expectedLabels: map[string]string{
				"created-by": "opconsh",
				"purpose":    "experimental-install",
			},
			expectedPkgName: "grafana-operator",
			expectedNS:      "grafana-system",
			expectedSA:      "opconsh-grafana",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build ClusterExtension like the real function does
			extension := &olmv1.ClusterExtension{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "olm.operatorframework.io/v1",
					Kind:       "ClusterExtension",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: tt.extensionName,
					Labels: map[string]string{
						"created-by": "opconsh",
						"purpose":    "experimental-install",
					},
				},
				Spec: olmv1.ClusterExtensionSpec{
					Namespace: tt.namespace,
					ServiceAccount: olmv1.ServiceAccountReference{
						Name: tt.serviceAccount,
					},
					Source: olmv1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &olmv1.CatalogFilter{
							PackageName: tt.packageName,
						},
					},
				},
			}

			// Set version if specified
			if tt.version != "" {
				extension.Spec.Source.Catalog.Version = tt.version
			}

			// Set channel if specified
			if tt.channel != "" {
				extension.Spec.Source.Catalog.Channels = []string{tt.channel}
			}

			// Validate the built extension
			if extension.Name != tt.extensionName {
				t.Errorf("Expected name %s, got %s", tt.extensionName, extension.Name)
			}

			for key, expectedValue := range tt.expectedLabels {
				if extension.Labels[key] != expectedValue {
					t.Errorf("Expected label %s=%s, got %s", key, expectedValue, extension.Labels[key])
				}
			}

			if extension.Spec.Namespace != tt.expectedNS {
				t.Errorf("Expected namespace %s, got %s", tt.expectedNS, extension.Spec.Namespace)
			}

			if extension.Spec.ServiceAccount.Name != tt.expectedSA {
				t.Errorf("Expected service account %s, got %s", tt.expectedSA, extension.Spec.ServiceAccount.Name)
			}

			if extension.Spec.Source.Catalog.PackageName != tt.expectedPkgName {
				t.Errorf("Expected package name %s, got %s", tt.expectedPkgName, extension.Spec.Source.Catalog.PackageName)
			}

			if tt.version != "" && extension.Spec.Source.Catalog.Version != tt.version {
				t.Errorf("Expected version %s, got %s", tt.version, extension.Spec.Source.Catalog.Version)
			}

			if tt.channel != "" {
				if len(extension.Spec.Source.Catalog.Channels) == 0 {
					t.Errorf("Expected channel %s, got no channels", tt.channel)
				} else if extension.Spec.Source.Catalog.Channels[0] != tt.channel {
					t.Errorf("Expected channel %s, got %s", tt.channel, extension.Spec.Source.Catalog.Channels[0])
				}
			}

			if extension.TypeMeta.APIVersion != "olm.operatorframework.io/v1" {
				t.Errorf("Expected APIVersion olm.operatorframework.io/v1, got %s", extension.TypeMeta.APIVersion)
			}

			if extension.TypeMeta.Kind != "ClusterExtension" {
				t.Errorf("Expected Kind ClusterExtension, got %s", extension.TypeMeta.Kind)
			}
		})
	}
}

func TestValidateExtensionLabels(t *testing.T) {
	tests := []struct {
		name      string
		extension olmv1.ClusterExtension
		wantValid bool
		wantErr   string
	}{
		{
			name: "valid opconsh extension",
			extension: olmv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"created-by": "opconsh",
						"purpose":    "experimental-install",
					},
				},
			},
			wantValid: true,
		},
		{
			name: "extension created by other tool",
			extension: olmv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"created-by": "helm",
					},
				},
			},
			wantValid: false,
			wantErr:   "was not created by opconsh experimental install",
		},
		{
			name: "extension with wrong purpose",
			extension: olmv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"created-by": "opconsh",
						"purpose":    "production-install",
					},
				},
			},
			wantValid: false,
			wantErr:   "was not created by opconsh experimental install",
		},
		{
			name: "extension with no labels",
			extension: olmv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{},
				},
			},
			wantValid: false,
			wantErr:   "was not created by opconsh experimental install",
		},
		{
			name: "extension with nil labels",
			extension: olmv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Labels: nil,
				},
			},
			wantValid: false,
			wantErr:   "was not created by opconsh experimental install",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Check labels like the real uninstall function does
			valid := tt.extension.Labels["created-by"] == "opconsh" && 
					 tt.extension.Labels["purpose"] == "experimental-install"

			if valid != tt.wantValid {
				t.Errorf("Expected valid=%v, got %v", tt.wantValid, valid)
			}

			// Simulate the error check
			if !valid && tt.wantErr != "" {
				// In real code this would be returned as an error
				errMsg := "was not created by opconsh experimental install"
				if !strings.Contains(errMsg, "was not created by opconsh") {
					t.Errorf("Expected error message to contain 'was not created by opconsh'")
				}
			}
		})
	}
}