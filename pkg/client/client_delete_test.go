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

func TestDeleteClusterExtension(t *testing.T) {
	scheme := runtime.NewScheme()
	olmv1.AddToScheme(scheme)

	tests := []struct {
		name           string
		extensionName  string
		existingExt    *olmv1.ClusterExtension
		wantErr        bool
	}{
		{
			name:          "delete existing cluster extension",
			extensionName: "test-extension",
			existingExt: &olmv1.ClusterExtension{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "olm.operatorframework.io/v1",
					Kind:       "ClusterExtension",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-extension",
				},
			},
			wantErr: false,
		},
		{
			name:          "delete non-existent cluster extension",
			extensionName: "non-existent",
			existingExt:   nil,
			wantErr:       false, // Should not error on non-existent resource
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dynamicClient dynamic.Interface
			if tt.existingExt != nil {
				dynamicClient = dynamicfake.NewSimpleDynamicClient(scheme, tt.existingExt)
			} else {
				dynamicClient = dynamicfake.NewSimpleDynamicClient(scheme)
			}

			client := &OLMClient{
				dynamic: dynamicClient,
			}

			err := client.DeleteClusterExtension(context.Background(), tt.extensionName)
			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteClusterExtension() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDeleteServiceAccount(t *testing.T) {
	tests := []struct {
		name        string
		namespace   string
		saName      string
		existingSA  *corev1.ServiceAccount
		wantErr     bool
		expectedErr string
	}{
		{
			name:      "delete service account created by opconsh",
			namespace: "test-ns",
			saName:    "test-sa",
			existingSA: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-sa",
					Namespace: "test-ns",
					Labels: map[string]string{
						"created-by": "opconsh",
						"purpose":    "experimental-extension-install",
					},
				},
			},
			wantErr: false,
		},
		{
			name:      "refuse to delete service account not created by opconsh",
			namespace: "test-ns",
			saName:    "other-sa",
			existingSA: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-sa",
					Namespace: "test-ns",
					Labels: map[string]string{
						"created-by": "other-tool",
					},
				},
			},
			wantErr:     true,
			expectedErr: "was not created by opconsh",
		},
		{
			name:       "delete non-existent service account",
			namespace:  "test-ns",
			saName:     "non-existent",
			existingSA: nil,
			wantErr:    false, // Should not error on non-existent resource
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

			err := client.DeleteServiceAccount(context.Background(), tt.namespace, tt.saName)
			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteServiceAccount() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.expectedErr != "" {
				if err == nil || len(tt.expectedErr) == 0 {
					t.Errorf("Expected error containing '%s', got %v", tt.expectedErr, err)
				}
			}

			// If deletion should succeed, verify the SA is gone
			if !tt.wantErr && tt.existingSA != nil {
				_, err := clientset.CoreV1().ServiceAccounts(tt.namespace).Get(context.Background(), tt.saName, metav1.GetOptions{})
				if err == nil {
					t.Errorf("Expected ServiceAccount to be deleted, but it still exists")
				}
			}
		})
	}
}

func TestDeleteClusterRoleBinding(t *testing.T) {
	tests := []struct {
		name        string
		crbName     string
		existingCRB *rbacv1.ClusterRoleBinding
		wantErr     bool
		expectedErr string
	}{
		{
			name:    "delete cluster role binding created by opconsh",
			crbName: "test-crb",
			existingCRB: &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-crb",
					Labels: map[string]string{
						"created-by": "opconsh",
						"purpose":    "experimental-extension-install",
					},
				},
			},
			wantErr: false,
		},
		{
			name:    "refuse to delete cluster role binding not created by opconsh",
			crbName: "other-crb",
			existingCRB: &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "other-crb",
					Labels: map[string]string{
						"created-by": "other-tool",
					},
				},
			},
			wantErr:     true,
			expectedErr: "was not created by opconsh",
		},
		{
			name:        "delete non-existent cluster role binding",
			crbName:     "non-existent",
			existingCRB: nil,
			wantErr:     false, // Should not error on non-existent resource
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

			err := client.DeleteClusterRoleBinding(context.Background(), tt.crbName)
			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteClusterRoleBinding() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.expectedErr != "" {
				if err == nil || len(tt.expectedErr) == 0 {
					t.Errorf("Expected error containing '%s', got %v", tt.expectedErr, err)
				}
			}

			// If deletion should succeed, verify the CRB is gone
			if !tt.wantErr && tt.existingCRB != nil {
				_, err := clientset.RbacV1().ClusterRoleBindings().Get(context.Background(), tt.crbName, metav1.GetOptions{})
				if err == nil {
					t.Errorf("Expected ClusterRoleBinding to be deleted, but it still exists")
				}
			}
		})
	}
}

func TestDeleteNamespaceIfEmpty(t *testing.T) {
	tests := []struct {
		name           string
		namespaceName  string
		existingNS     *corev1.Namespace
		existingPods   []corev1.Pod
		existingServices []corev1.Service
		wantErr        bool
		expectDeleted  bool
		expectedErr    string
	}{
		{
			name:          "delete empty namespace created by opconsh",
			namespaceName: "test-ns",
			existingNS: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
					Labels: map[string]string{
						"created-by": "opconsh",
						"purpose":    "experimental-extension-install",
					},
				},
			},
			existingPods:   []corev1.Pod{},
			existingServices: []corev1.Service{},
			wantErr:       false,
			expectDeleted: true,
		},
		{
			name:          "refuse to delete namespace not created by opconsh",
			namespaceName: "other-ns",
			existingNS: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "other-ns",
					Labels: map[string]string{
						"created-by": "other-tool",
					},
				},
			},
			wantErr:     true,
			expectedErr: "was not created by opconsh",
		},
		{
			name:          "don't delete namespace with pods",
			namespaceName: "test-ns-with-pods",
			existingNS: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns-with-pods",
					Labels: map[string]string{
						"created-by": "opconsh",
						"purpose":    "experimental-extension-install",
					},
				},
			},
			existingPods: []corev1.Pod{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns-with-pods",
				},
			}},
			wantErr:       false,
			expectDeleted: false, // Should not delete due to pods
		},
		{
			name:          "don't delete namespace with services",
			namespaceName: "test-ns-with-services",
			existingNS: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns-with-services",
					Labels: map[string]string{
						"created-by": "opconsh",
						"purpose":    "experimental-extension-install",
					},
				},
			},
			existingServices: []corev1.Service{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "test-ns-with-services",
				},
			}},
			wantErr:       false,
			expectDeleted: false, // Should not delete due to services
		},
		{
			name:          "delete non-existent namespace",
			namespaceName: "non-existent",
			existingNS:    nil,
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := []runtime.Object{}
			if tt.existingNS != nil {
				objects = append(objects, tt.existingNS)
			}
			for i := range tt.existingPods {
				objects = append(objects, &tt.existingPods[i])
			}
			for i := range tt.existingServices {
				objects = append(objects, &tt.existingServices[i])
			}

			clientset := kubefake.NewClientset(objects...)

			client := &OLMClient{
				clientset: clientset,
			}

			err := client.DeleteNamespaceIfEmpty(context.Background(), tt.namespaceName)
			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteNamespaceIfEmpty() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.expectedErr != "" {
				if err == nil || len(tt.expectedErr) == 0 {
					t.Errorf("Expected error containing '%s', got %v", tt.expectedErr, err)
				}
			}

			// Check if namespace was deleted as expected
			if tt.existingNS != nil && !tt.wantErr {
				_, err := clientset.CoreV1().Namespaces().Get(context.Background(), tt.namespaceName, metav1.GetOptions{})
				if tt.expectDeleted {
					if err == nil {
						t.Errorf("Expected namespace to be deleted, but it still exists")
					}
				} else {
					if err != nil {
						t.Errorf("Expected namespace to be preserved, but it was deleted")
					}
				}
			}
		})
	}
}