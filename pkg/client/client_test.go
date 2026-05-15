package client

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"

	olmv1 "github.com/operator-framework/operator-controller/api/v1"
)

func TestCreateNamespace(t *testing.T) {
	tests := []struct {
		name          string
		namespaceName string
		existingNS    *corev1.Namespace
		wantErr       bool
	}{
		{
			name:          "create new namespace",
			namespaceName: "test-ns",
			existingNS:    nil,
			wantErr:       false,
		},
		{
			name:          "namespace already exists",
			namespaceName: "existing-ns",
			existingNS: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "existing-ns",
				},
			},
			wantErr: false, // Should not error on existing namespace
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientset := kubefake.NewClientset()
			
			// Add existing namespace if specified
			if tt.existingNS != nil {
				clientset = kubefake.NewClientset(tt.existingNS)
			}

			client := &OLMClient{
				clientset: clientset,
			}

			err := client.CreateNamespace(context.Background(), tt.namespaceName)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateNamespace() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Verify namespace was created with correct labels (only for new namespaces)
			if !tt.wantErr && tt.existingNS == nil {
				ns, err := clientset.CoreV1().Namespaces().Get(context.Background(), tt.namespaceName, metav1.GetOptions{})
				if err != nil {
					t.Errorf("Failed to get created namespace: %v", err)
					return
				}

				if ns.Labels["created-by"] != "opconsh" {
					t.Errorf("Expected created-by label to be 'opconsh', got %s", ns.Labels["created-by"])
				}
				if ns.Labels["purpose"] != "experimental-extension-install" {
					t.Errorf("Expected purpose label to be 'experimental-extension-install', got %s", ns.Labels["purpose"])
				}
			}
		})
	}
}

func TestCreateServiceAccount(t *testing.T) {
	tests := []struct {
		name        string
		namespace   string
		saName      string
		existingSA  *corev1.ServiceAccount
		wantErr     bool
	}{
		{
			name:       "create new service account",
			namespace:  "test-ns",
			saName:     "test-sa",
			existingSA: nil,
			wantErr:    false,
		},
		{
			name:      "service account already exists",
			namespace: "test-ns",
			saName:    "existing-sa",
			existingSA: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "existing-sa",
					Namespace: "test-ns",
				},
			},
			wantErr: false, // Should not error on existing SA
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var clientset kubernetes.Interface
			if tt.existingSA != nil {
				clientset = kubefake.NewClientset(tt.existingSA)
			} else {
				clientset = kubefake.NewClientset()
			}

			client := &OLMClient{
				clientset: clientset,
			}

			err := client.CreateServiceAccount(context.Background(), tt.namespace, tt.saName)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateServiceAccount() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Verify service account was created with correct labels (only for new SAs)
			if !tt.wantErr && tt.existingSA == nil {
				sa, err := clientset.CoreV1().ServiceAccounts(tt.namespace).Get(context.Background(), tt.saName, metav1.GetOptions{})
				if err != nil {
					t.Errorf("Failed to get created service account: %v", err)
					return
				}

				if sa.Labels["created-by"] != "opconsh" {
					t.Errorf("Expected created-by label to be 'opconsh', got %s", sa.Labels["created-by"])
				}
				if sa.Labels["purpose"] != "experimental-extension-install" {
					t.Errorf("Expected purpose label to be 'experimental-extension-install', got %s", sa.Labels["purpose"])
				}
			}
		})
	}
}

func TestCreateClusterRoleBinding(t *testing.T) {
	tests := []struct {
		name         string
		crbName      string
		saName       string
		saNamespace  string
		existingCRB  *rbacv1.ClusterRoleBinding
		wantErr      bool
	}{
		{
			name:        "create new cluster role binding",
			crbName:     "test-crb",
			saName:      "test-sa",
			saNamespace: "test-ns",
			existingCRB: nil,
			wantErr:     false,
		},
		{
			name:        "cluster role binding already exists",
			crbName:     "existing-crb",
			saName:      "test-sa",
			saNamespace: "test-ns",
			existingCRB: &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "existing-crb",
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     "cluster-admin",
				},
				Subjects: []rbacv1.Subject{{
					Kind:      "ServiceAccount",
					Name:      "test-sa",
					Namespace: "test-ns",
				}},
			},
			wantErr: false, // Should not error on existing CRB
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var clientset kubernetes.Interface
			if tt.existingCRB != nil {
				clientset = kubefake.NewClientset(tt.existingCRB)
			} else {
				clientset = kubefake.NewClientset()
			}

			client := &OLMClient{
				clientset: clientset,
			}

			err := client.CreateClusterRoleBinding(context.Background(), tt.crbName, tt.saName, tt.saNamespace)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateClusterRoleBinding() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Verify cluster role binding was created with correct properties (only for new CRBs)
			if !tt.wantErr && tt.existingCRB == nil {
				crb, err := clientset.RbacV1().ClusterRoleBindings().Get(context.Background(), tt.crbName, metav1.GetOptions{})
				if err != nil {
					t.Errorf("Failed to get created cluster role binding: %v", err)
					return
				}

				if crb.Labels["created-by"] != "opconsh" {
					t.Errorf("Expected created-by label to be 'opconsh', got %s", crb.Labels["created-by"])
				}
				if crb.Labels["purpose"] != "experimental-extension-install" {
					t.Errorf("Expected purpose label to be 'experimental-extension-install', got %s", crb.Labels["purpose"])
				}
				if crb.RoleRef.Name != "cluster-admin" {
					t.Errorf("Expected RoleRef.Name to be 'cluster-admin', got %s", crb.RoleRef.Name)
				}
				if len(crb.Subjects) != 1 || crb.Subjects[0].Name != tt.saName || crb.Subjects[0].Namespace != tt.saNamespace {
					t.Errorf("ClusterRoleBinding subjects not configured correctly")
				}
			}
		})
	}
}

func TestCreateClusterExtension(t *testing.T) {
	scheme := runtime.NewScheme()
	olmv1.AddToScheme(scheme)

	tests := []struct {
		name      string
		extension *olmv1.ClusterExtension
		wantErr   bool
	}{
		{
			name: "create cluster extension",
			extension: &olmv1.ClusterExtension{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "olm.operatorframework.io/v1",
					Kind:       "ClusterExtension",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-extension",
					Labels: map[string]string{
						"created-by": "opconsh",
						"purpose":    "experimental-install",
					},
				},
				Spec: olmv1.ClusterExtensionSpec{
					Namespace: "test-ns",
					ServiceAccount: olmv1.ServiceAccountReference{
						Name: "opconsh-test-extension",
					},
					Source: olmv1.SourceConfig{
						SourceType: "Catalog",
						Catalog: &olmv1.CatalogFilter{
							PackageName: "test-package",
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dynamicClient dynamic.Interface = dynamicfake.NewSimpleDynamicClient(scheme)

			client := &OLMClient{
				dynamic: dynamicClient,
			}

			err := client.CreateClusterExtension(context.Background(), tt.extension)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateClusterExtension() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify the extension was created (basic check - dynamic client testing is limited)
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
			}
		})
	}
}