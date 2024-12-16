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
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/pkg/errors"
	apiv1alpha1 "github.com/trevex/api-publish-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// APIResourceDefinitionReconciler reconciles a APIResourceDefinition object
type APIResourceDefinitionReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	EventRecorder record.EventRecorder
}

var (
	definitionNameAnnotation      = fmt.Sprintf("%s/definition-name", apiv1alpha1.GroupVersion.Group)
	definitionNamespaceAnnotation = fmt.Sprintf("%s/definition-namespace", apiv1alpha1.GroupVersion.Group)
)

// +kubebuilder:rbac:groups=api.kovo.li,resources=apiresourcedefinitions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=api.kovo.li,resources=apiresourcedefinitions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=api.kovo.li,resources=apiresourcedefinitions/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the APIResourceDefinition object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *APIResourceDefinitionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.Info("reconciling resource")

	ard := &apiv1alpha1.APIResourceDefinition{}
	if err := r.Get(ctx, req.NamespacedName, ard); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, errors.Wrap(err, "could not get APIResourceDefinition")
		}
		return ctrl.Result{}, nil
	}

	// Check, if CRD is already in place
	logger.Info("fetching CRD")
	crdExists := false
	crd := &apiextensionsv1.CustomResourceDefinition{}
	crdName := fmt.Sprintf("%s.%s", ard.Spec.Names.Plural, ard.Spec.Group)

	if err := r.Get(ctx, client.ObjectKey{Name: crdName}, crd); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, errors.Wrap(err, "could not get requested CRD")
		}
		crdExists = false
	} else {
		crdExists = true
	}

	crdOwnedByUs, _ := isOwned(crd, req.NamespacedName, definitionNameAnnotation, definitionNamespaceAnnotation)

	// if resource is marked for deletion
	if ard.DeletionTimestamp != nil {
		logger.Info("resource is marked for deletion", "APIResourceDefinition", ard.Name)

		// if requested CRD exists and we own it
		if crdExists && crdOwnedByUs {
			// check, for all versions of the CRD, if resources of this kind still exist in the cluster
			for _, version := range ard.Spec.Versions {
				gvk := schema.GroupVersionKind{
					Group:   ard.Spec.Group,
					Version: version.Name,
					Kind:    ard.Spec.Names.Kind,
				}

				resourceList := &unstructured.UnstructuredList{}
				resourceList.SetGroupVersionKind(gvk)

				if err := r.List(ctx, resourceList); err != nil {
					return ctrl.Result{}, errors.Wrap(err, "error while fetching the list of resources for requested CRD")
				}

				// if resources of that kind have been found
				if len(resourceList.Items) != 0 {
					resourceNames := []types.NamespacedName{}

					for _, resource := range resourceList.Items {
						resourceNames = append(resourceNames, types.NamespacedName{
							Name:      resource.GetName(),
							Namespace: resource.GetNamespace(),
						})
					}

					logger.Info("there are still resources of CRD left, denying deletion request", "kind:", ard.Spec.Names.Kind, "list:", resourceNames)
					condition := metav1.Condition{
						Type:    "DeletionApproved",
						Status:  "False",
						Reason:  "CRstillExists",
						Message: fmt.Sprintf("resources of kind %s are sill existent, deletion of APIResourceDefinition denied", gvk.Kind),
					}

					if err := updateConditions(ctx, r.Client, req.NamespacedName, ard, &ard.Status.Conditions, condition); err != nil {
						return ctrl.Result{}, errors.Wrap(err, "unable to update status of APIResourceDefinition")
					}

					r.EventRecorder.Eventf(ard, corev1.EventTypeWarning, "DeletionBlocked",
						"Resource deletion is blocked because dependent resources of kind %s still exist", ard.Spec.Names.Kind)

				}
			}
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *APIResourceDefinitionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.EventRecorder = mgr.GetEventRecorderFor("apiresourcedefinition-controller")
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1alpha1.APIResourceDefinition{}).
		Named("apiresourcedefinition").
		Complete(r)
}
