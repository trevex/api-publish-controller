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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
		testAPIGroup      = "stable.example.com"
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
			Namespace: resourceNamespace,
		},
		Spec: apiv1alpha1.APIResourceDefinitionSpec{
			APIResourceSchemaSpec: kcpv1alpha1.APIResourceSchemaSpec{
				Group: testAPIGroup,
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
								&apiextensionsv1.CustomResourceValidation{
									OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
										Type: "object",
									},
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

	instance := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":      "testresource",
				"namespace": resourceNamespace,
			},
		},
	}

	gvk := schema.GroupVersionKind{
		Group:   ard.Spec.APIResourceSchemaSpec.Group,
		Version: ard.Spec.APIResourceSchemaSpec.Versions[0].Name,
		Kind:    ard.Spec.APIResourceSchemaSpec.Names.Kind,
	}

	instance.SetGroupVersionKind(gvk)

	agr := &apiv1alpha1.APIGroupRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testAPIGroup,
			Namespace: resourceNamespace,
		},
	}

	namespacedNameAGR := types.NamespacedName{
		Name:      testAPIGroup,
		Namespace: resourceNamespace,
	}

	Context("reconciling a resource marked for deletion", func() {

		ctx := context.Background()

		BeforeEach(func() {
			By("creating the custom resource for the Kind APIResourceDefinition")
			ardTmp := ard.DeepCopy()
			err := k8sClient.Get(ctx, typeNamespacedName, ardTmp)
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, ardTmp)).To(Succeed())
			}

			By("adding a finalizer to the APIResourceDefinition resource")
			Expect(controllerutil.AddFinalizer(ardTmp, finalizerName)).To(BeTrue())
			Expect(k8sClient.Update(ctx, ardTmp)).To(Succeed())

			By("creating ServiceAccount")
			saTmp := sa.DeepCopy()
			Expect(k8sClient.Create(ctx, saTmp)).To(Succeed())

			By("creating ClusterRole")
			crTmp := cr.DeepCopy()
			Expect(k8sClient.Create(ctx, crTmp)).To(Succeed())

			By("creating ClusterRoleBinding")
			crbTmp := crb.DeepCopy()
			Expect(k8sClient.Create(ctx, crbTmp)).To(Succeed())

			By("creating CRD")
			crdTmp := crd.DeepCopy()
			Expect(k8sClient.Create(ctx, crdTmp)).To(Succeed())

			By("deleting the APIResourceDefinition resource")
			Expect(k8sClient.Delete(ctx, ard)).To(Succeed())

		})

		AfterEach(func() {
			By("deleting ServiceAccount")
			Expect(k8sClient.Delete(ctx, sa)).To(Succeed())
		})

		It("should successfully delete the resource", func() {
			By("reconciling the created resource")
			eventRecorder := record.NewFakeRecorder(10)
			reconcileARD(ctx, k8sClient, typeNamespacedName, eventRecorder)

			By("checking, if APIResourceDefinition is gone")
			ardTmp := &apiv1alpha1.APIResourceDefinition{}
			err = k8sClient.Get(ctx, typeNamespacedName, ardTmp)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			By("checking, if ClusterRole is gone")
			crTmp := &rbacv1.ClusterRole{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: crName}, crTmp)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			By("checking, if ClusterRoleBinding is gone")
			crbTmp := &rbacv1.ClusterRoleBinding{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: crbName}, crbTmp)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			By("checking, if CRD is gone")
			Eventually(func() bool {
				crdTmp := &apiextensionsv1.CustomResourceDefinition{}
				err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, crdTmp)
				return errors.IsNotFound(err)
			}).Should(BeTrue())
		})

		It("should successfully block deletion, if CR still exists", func() {
			By("Adding a CR resource")
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			By("reconciling the created resource")
			eventRecorder := record.NewFakeRecorder(10)
			reconcileARD(ctx, k8sClient, typeNamespacedName, eventRecorder)
			close(eventRecorder.Events)

			By("checking, if ARD resource is still there")
			ardTmp := &apiv1alpha1.APIResourceDefinition{}
			err := k8sClient.Get(ctx, typeNamespacedName, ardTmp)
			Expect(err).ToNot(HaveOccurred())

			By("Checking, if blocked event has been created")
			events := []string{}
			for event := range eventRecorder.Events {
				events = append(events, event)
			}
			Expect(events).To(ContainElement(ContainSubstring("DeletionBlocked")))

			By("Deleting the CR resource")
			Expect(k8sClient.Delete(ctx, instance)).To(Succeed())

			By("Reconciling again to remove all resources")
			reconcileARD(ctx, k8sClient, typeNamespacedName, record.NewFakeRecorder(10))
		})
	})

	Context("When reconciling a resource", func() {

		ctx := context.Background()

		BeforeEach(func() {
			By("creating the custom resource for the Kind APIResourceDefinition")
			ardTmp := ard.DeepCopy()
			err := k8sClient.Get(ctx, typeNamespacedName, ardTmp)
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, ardTmp)).To(Succeed())
			}

			By("creating ServiceAccount")
			saTmp := sa.DeepCopy()
			Expect(k8sClient.Create(ctx, saTmp)).To(Succeed())

			By("Creating APIGroupRequest, if necessary")
			agrTmp := agr.DeepCopy()
			err = k8sClient.Get(ctx, namespacedNameAGR, agrTmp)
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, agr)).To(Succeed())
				By("Reconciling the APIGroupRequest")
				reconcileAGR(ctx, k8sClient, namespacedNameAGR)

				err = k8sClient.Get(ctx, namespacedNameAGR, agrTmp)
				Expect(err).ToNot(HaveOccurred())
				checkStatus(agrTmp.Status.Conditions, "APIGroupReserved", "ClusterAPIGroupCreated", BeTrue())

			}

			Expect(err).ToNot(HaveOccurred())

		})

		AfterEach(func() {
			By("deleting ServiceAccount")
			Expect(k8sClient.Delete(ctx, sa)).To(Succeed())
		})

		It("should successfully reconcile the resource", func() {
			By("reconciling the created resource")
			eventRecorder := record.NewFakeRecorder(10)
			reconcileARD(ctx, k8sClient, typeNamespacedName, eventRecorder)
			close(eventRecorder.Events)

			By("checking, if CRD is created")
			Eventually(func() bool {
				crdTmp := &apiextensionsv1.CustomResourceDefinition{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceName}, crdTmp)
				return err == nil
			}).Should(BeTrue())

			By("checking, if ClusterRole is created")
			Eventually(func() bool {
				crTmp := &rbacv1.ClusterRole{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: crName}, crTmp)
				return err == nil
			}).Should(BeTrue())

			By("checking, if ClusterRoleBinding is created")
			Eventually(func() bool {
				crbTmp := &rbacv1.ClusterRoleBinding{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: crbName}, crbTmp)
				return err == nil
			}).Should(BeTrue())

			By("Checking, if ARD has the correct status and finalizer")
			ardTmp := &apiv1alpha1.APIResourceDefinition{}
			err := k8sClient.Get(ctx, typeNamespacedName, ardTmp)
			Expect(err).ToNot(HaveOccurred())
			checkStatus(ardTmp.Status.Conditions, "CRDDeployed", "CRDCreated", BeTrue())
			Expect(ardTmp.GetFinalizers()).To(ContainElement(finalizerName))

			By("Deleting all created resources again")
			Expect(k8sClient.Delete(ctx, ard)).To(Succeed())
		})
	})
})

func jsonOrDie(obj interface{}) []byte {
	ret, err := json.Marshal(obj)
	if err != nil {
		panic(err)
	}

	return ret
}

func reconcileARD(ctx context.Context, client client.Client, namespacedName types.NamespacedName, recorder record.EventRecorder) {
	controllerReconciler := &APIResourceDefinitionReconciler{
		Client:        client,
		Scheme:        client.Scheme(),
		EventRecorder: recorder,
	}
	_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: namespacedName,
	})
	Expect(err).NotTo(HaveOccurred())
}

// func checkIfResourcesAreGone
