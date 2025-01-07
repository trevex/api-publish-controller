/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kcpv1alpha1 "github.com/kcp-dev/kcp/sdk/apis/apis/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1alpha1 "github.com/trevex/api-publish-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
)

var _ = Describe("APIResourceDefinition Controller", func() {

	const (
		resourceName      = "shirts.stable.example.com"
		resourceNamespace = "default"
	)

	var (
		saName  = fmt.Sprintf("%s-sa", resourceName)
		crName  = fmt.Sprintf("%s-role", resourceName)
		crbName = fmt.Sprintf("%s-rolebinding", resourceName)
	)

	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: resourceNamespace,
	}

	// defining test resources
	ard := &apiv1alpha1.APIResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: "default",
		},
		Spec: apiv1alpha1.APIResourceDefinitionSpec{
			APIResourceSchemaSpec: kcpv1alpha1.APIResourceSchemaSpec{
				Group: "stable.example.com",
				Scope: apiextensionsv1.NamespaceScoped,
				Names: apiextensionsv1.CustomResourceDefinitionNames{
					Plural:   "shirts",
					Singular: "shirt",
					Kind:     "Shirt",
				},
				Versions: []kcpv1alpha1.APIResourceVersion{
					{
						Name:    "v1",
						Served:  true,
						Storage: true,
						Schema: runtime.RawExtension{
							Raw: jsonOrDie(
								&apiextensionsv1.JSONSchemaProps{
									Type: "object",
								},
							),
						},
					},
				},
			},
			ServiceAccountRef: &corev1.LocalObjectReference{
				Name: saName,
			},
		},
	}

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      saName,
			Namespace: resourceNamespace,
		},
	}

	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: crName,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"stable.example.com"},
				Resources: []string{"shirts"},
				Verbs:     []string{"list", "get", "watch"},
			},
		},
	}

	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: crbName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      saName,
				Namespace: resourceNamespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     crName,
		},
	}

	crd, err := createCRDfromARD(ard)
	Expect(err).ToNot(HaveOccurred())

	annotations := map[string]string{
		definitionNameAnnotation:      resourceName,
		definitionNamespaceAnnotation: resourceNamespace,
	}

	crd.SetAnnotations(annotations)

	Context("reconciling a resource marked for deletion", func() {

		ctx := context.Background()

		BeforeEach(func() {
			By("creating the custom resource for the Kind APIResourceDefinition")
			err := k8sClient.Get(ctx, typeNamespacedName, ard)
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, ard)).To(Succeed())
			}

			By("adding a finalizer to the APIResourceDefinition resource")
			Expect(controllerutil.AddFinalizer(ard, finalizerName)).To(BeTrue())
			Expect(k8sClient.Update(ctx, ard)).To(Succeed())

			By("creating ServiceAccount")
			Expect(k8sClient.Create(ctx, sa)).To(Succeed())

			By("creating ClusterRole")
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			By("creating ClusterRoleBinding")
			Expect(k8sClient.Create(ctx, crb)).To(Succeed())

			By("creating CRD")
			Expect(k8sClient.Create(ctx, crd)).To(Succeed())

			By("deleting the APIResourceDefinition resource")
			Expect(k8sClient.Delete(ctx, ard)).To(Succeed())

		})

		It("should successfully delete the resource", func() {
			By("reconciling the created resource")
			controllerReconciler := &APIResourceDefinitionReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("checking, if APIResourceDefinition is gone")
			err = k8sClient.Get(ctx, typeNamespacedName, ard)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			By("checking, if ClusterRole is gone")
			err = k8sClient.Get(ctx, types.NamespacedName{Name: crName}, cr)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			By("checking, if ClusterRoleBinding is gone")
			err = k8sClient.Get(ctx, types.NamespacedName{Name: crbName}, crb)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			By("checking, if CRD is gone")
			Eventually(func() bool {
				err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, crd)
				return errors.IsNotFound(err)
			}).Should(BeTrue())
		})
	})

	// Context("When reconciling a resource", func() {
	// 	const resourceName = "shirts.stable.example.com"

	// 	ctx := context.Background()

	// 	typeNamespacedName := types.NamespacedName{
	// 		Name:      resourceName,
	// 		Namespace: "default",
	// 	}
	// 	ard := &apiv1alpha1.APIResourceDefinition{}

	// 	BeforeEach(func() {
	// 		By("creating the custom resource for the Kind APIResourceDefinition")
	// 		err := k8sClient.Get(ctx, typeNamespacedName, ard)
	// 		if err != nil && errors.IsNotFound(err) {
	// 			resource := &apiv1alpha1.APIResourceDefinition{
	// 				ObjectMeta: metav1.ObjectMeta{
	// 					Name:      resourceName,
	// 					Namespace: "default",
	// 				},
	// 				Spec: apiv1alpha1.APIResourceDefinitionSpec{
	// 					APIResourceSchemaSpec: kcpv1alpha1.APIResourceSchemaSpec{
	// 						Group: "stable.example.com",
	// 						Scope: apiextensionsv1.NamespaceScoped,
	// 						Names: apiextensionsv1.CustomResourceDefinitionNames{
	// 							Plural:   "shirts",
	// 							Singular: "shirt",
	// 							Kind:     "Shirt",
	// 						},
	// 						Versions: []kcpv1alpha1.APIResourceVersion{
	// 							{
	// 								Name:    "v1",
	// 								Served:  true,
	// 								Storage: true,
	// 								Schema: runtime.RawExtension{
	// 									Raw: jsonOrDie(
	// 										&apiextensionsv1.JSONSchemaProps{
	// 											Type: "object",
	// 										},
	// 									),
	// 								},
	// 							},
	// 						},
	// 					},
	// 				}}
	// 			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
	// 		}
	// 	})

	// 	AfterEach(func() {
	// 		// TODO(user): Cleanup logic after each test, like removing the resource instance.
	// 		resource := &apiv1alpha1.APIResourceDefinition{}
	// 		err := k8sClient.Get(ctx, typeNamespacedName, resource)
	// 		Expect(err).NotTo(HaveOccurred())

	// 		By("cleanup the specific resource instance APIResourceDefinition")
	// 		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	// 	})

	// 	It("should successfully reconcile the resource", func() {
	// 		By("reconciling the created resource")
	// 		controllerReconciler := &APIResourceDefinitionReconciler{
	// 			Client: k8sClient,
	// 			Scheme: k8sClient.Scheme(),
	// 		}

	// 		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
	// 			NamespacedName: typeNamespacedName,
	// 		})
	// 		Expect(err).NotTo(HaveOccurred())
	// 		// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
	// 		// Example: If you expect a certain status condition after reconciliation, verify it here.
	// 	})
	// })
})

func jsonOrDie(obj interface{}) []byte {
	ret, err := json.Marshal(obj)
	if err != nil {
		panic(err)
	}

	return ret
}
