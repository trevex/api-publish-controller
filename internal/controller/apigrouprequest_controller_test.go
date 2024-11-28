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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1alpha1 "github.com/trevex/api-publish-controller/api/v1alpha1"
)

var _ = Describe("APIGroupRequest Controller", func() {

	const (
		resourceName        = "test-apigroup"
		clusterApiGroupName = "test-clusterapigroup"
		namespace           = "default"
	)

	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: namespace,
	}

	namespacedNameClusterApiGroup := types.NamespacedName{
		Name:      clusterApiGroupName,
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
					Name: clusterApiGroupName,
				}
				annotations := map[string]string{
					"api.kovo.li/request-name":      resourceName,
					"api.kovo.li/request-namespace": namespace,
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
			Expect(k8sClient.Get(ctx, namespacedNameClusterApiGroup, clusterApiGroup)).To(Succeed())
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

	// Context("reconciling a resource ...", func() {

	// 	ctx := context.Background()

	// 	BeforeEach(func() {
	// 		By("creating the custom resource for the Kind APIGroupRequest")
	// 		err := k8sClient.Get(ctx, typeNamespacedName, apigrouprequest)
	// 		if err != nil && errors.IsNotFound(err) {
	// 			resource := &apiv1alpha1.APIGroupRequest{
	// 				ObjectMeta: metav1.ObjectMeta{
	// 					Name:      resourceName,
	// 					Namespace: "default",
	// 				},
	// 				// TODO(user): Specify other spec details if needed.
	// 			}
	// 			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
	// 		}
	// 	})

	// 	AfterEach(func() {
	// 		// TODO(user): Cleanup logic after each test, like removing the resource instance.
	// 		resource := &apiv1alpha1.APIGroupRequest{}
	// 		err := k8sClient.Get(ctx, typeNamespacedName, resource)
	// 		Expect(err).NotTo(HaveOccurred())

	// 		By("Cleanup the specific resource instance APIGroupRequest")
	// 		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	// 	})

	// 	It("should successfully reconcile the resource", func() {
	// 		By("Reconciling the created resource")
	// 		controllerReconciler := &APIGroupRequestReconciler{
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
