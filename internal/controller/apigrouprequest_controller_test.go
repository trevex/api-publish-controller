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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1alpha1 "github.com/trevex/api-publish-controller/api/v1alpha1"
)

var _ = Describe("APIGroupRequest Controller", func() {

	const (
		resourceName = "test-apigroup"
		namespace    = "default"
	)

	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: namespace,
	}

	apiGroupRequest := &apiv1alpha1.APIGroupRequest{}
	clusterApiGroup := &apiv1alpha1.ClusterAPIGroup{}

	Context("reconciling a resource marked for deletion", func() {

		ctx := context.Background()

		BeforeEach(func() {

			By("creating the custom resource for the Kind APIGroupRequest")
			err := k8sClient.Get(ctx, typeNamespacedName, apiGroupRequest)
			if err != nil && errors.IsNotFound(err) {
				apiGroupRequest.ObjectMeta = metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				}
				Expect(k8sClient.Create(ctx, apiGroupRequest)).To(Succeed())
			}

			By("adding a finalizer to the APIGroupRequest resource")
			Expect(controllerutil.AddFinalizer(apiGroupRequest, finalizerName)).To(BeTrue())
			Expect(k8sClient.Update(ctx, apiGroupRequest)).To(Succeed())

			By("creating the custom resource for the Kind ClusterAPIGroup")
			err = k8sClient.Get(ctx, typeNamespacedName, clusterApiGroup)
			if err != nil && errors.IsNotFound(err) {
				clusterApiGroup.ObjectMeta = metav1.ObjectMeta{
					Name: resourceName,
				}
				annotations := map[string]string{
					requestNameAnnotation:      resourceName,
					requestNamespaceAnnotation: namespace,
				}
				clusterApiGroup.SetAnnotations(annotations)
				Expect(k8sClient.Create(ctx, clusterApiGroup)).To(Succeed())
			}

			By("deleting the APIGroupRequest resource")
			Expect(k8sClient.Delete(ctx, apiGroupRequest)).To(Succeed())

		})

		// AfterEach block is not needed in this context, as the resources should be cleaned up during this spec

		It("should successfully reconcile the resources", func() {

			By("checking if needed resources are there")
			Expect(k8sClient.Get(ctx, typeNamespacedName, clusterApiGroup)).To(Succeed())
			Expect(k8sClient.Get(ctx, typeNamespacedName, apiGroupRequest)).To(Succeed())

			By("reconciling the created resource")
			controllerReconciler := &APIGroupRequestReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("checking for the ClusterAPIGroup resource")
			err = k8sClient.Get(ctx, typeNamespacedName, clusterApiGroup)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			By("checking for the APIGroupRequest resource")
			err = k8sClient.Get(ctx, typeNamespacedName, apiGroupRequest)
			Expect(errors.IsNotFound(err)).To(BeTrue())

		})
	})

	Context("reconciling a resource", func() {

		ctx := context.Background()

		BeforeEach(func() {
			By("creating the custom resource for the Kind APIGroupRequest")
			err := k8sClient.Get(ctx, typeNamespacedName, apiGroupRequest)
			if err != nil && errors.IsNotFound(err) {
				resource := &apiv1alpha1.APIGroupRequest{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &apiv1alpha1.APIGroupRequest{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance APIGroupRequest")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			By("Reconciling the resource to delete it successfully")
			reconcileResource(ctx, k8sClient, typeNamespacedName)

			By("Checking, if resource is gone after deleting it")
			err = k8sClient.Get(ctx, typeNamespacedName, apiGroupRequest)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})

		It("should successfully reconcile the resource, if a ClusterAPIGroup owned by someone else exists", func() {
			By("Creating a ClusterAPIGroup owned by someone else")
			cagr := &apiv1alpha1.ClusterAPIGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: resourceName,
					Annotations: map[string]string{
						requestNameAnnotation:      "someOtherName",
						requestNamespaceAnnotation: "someOtherNamespace",
					},
				},
			}
			Expect(k8sClient.Create(ctx, cagr)).To(Succeed())

			By("Reconciling the created resource")
			reconcileResource(ctx, k8sClient, typeNamespacedName)

			By("Checking, if status of APIGroupRequest resource is set correctly")
			agr := &apiv1alpha1.APIGroupRequest{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, agr)).To(Succeed())
			checkStatus(agr.Status.Conditions, "APIGroupReserved", "ClusterAPIGroupOwnedByOthers", BeFalse())

			By("Deleting ClusterAPIGroup again, which is not owned by us")
			Expect(k8sClient.Delete(ctx, cagr)).To(Succeed())
		})

		It("should successfully reconcile the resource, if ClusterAPIGroup already exists and is owned by us", func() {
			By("Creating a ClusterAPIGroup owned by us")
			cagr := &apiv1alpha1.ClusterAPIGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: resourceName,
					Annotations: map[string]string{
						requestNameAnnotation:      resourceName,
						requestNamespaceAnnotation: namespace,
					},
				},
			}
			Expect(k8sClient.Create(ctx, cagr)).To(Succeed())

			By("Reconciling the created resource")
			reconcileResource(ctx, k8sClient, typeNamespacedName)

			By("Checking, if status of APIGroupRequest resource is set correctly")
			agr := &apiv1alpha1.APIGroupRequest{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, agr)).To(Succeed())
			checkStatus(agr.Status.Conditions, "APIGroupReserved", "ClusterAPIGroupAlreadyOwned", BeTrue())
		})

		It("should successfully reconcile the resource, if ClusterAPIGroup does not already exist", func() {
			By("Reconciling the created resource")
			reconcileResource(ctx, k8sClient, typeNamespacedName)

			By("Checking, if corresponding ClusterAPIGroup exists")
			cagr := &apiv1alpha1.ClusterAPIGroup{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, cagr)).To(Succeed())

			By("Checking, if corresponding ClusterAPIGroup has correct owner annotations")
			ownedByUs, _ := isOwned(cagr, typeNamespacedName, requestNameAnnotation, requestNamespaceAnnotation)
			Expect(ownedByUs).To(BeTrue())

			By("Checking, if status of APIGroupRequest resource is set correctly")
			agr := &apiv1alpha1.APIGroupRequest{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, agr)).To(Succeed())
			checkStatus(agr.Status.Conditions, "APIGroupReserved", "ClusterAPIGroupCreated", BeTrue())
		})
	})
})

func reconcileResource(ctx context.Context, client client.Client, namespacedName types.NamespacedName) {
	controllerReconciler := &APIGroupRequestReconciler{
		Client: client,
		Scheme: client.Scheme(),
	}
	_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: namespacedName,
	})
	Expect(err).NotTo(HaveOccurred())
}
