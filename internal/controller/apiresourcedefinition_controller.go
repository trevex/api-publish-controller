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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/pkg/errors"
	apiv1alpha1 "github.com/trevex/api-publish-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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

	clusterRoleName := fmt.Sprintf("%s-role", ard.Name)
	clusterRoleBindingName := fmt.Sprintf("%s-rolebinding", ard.Name)

	// define ClusterRole and ClusterRoleBinding
	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterRoleName,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{ard.Spec.APIResourceSchemaSpec.Group},
				Resources: []string{ard.Spec.APIResourceSchemaSpec.Names.Plural},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}

	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterRoleBindingName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      ard.Spec.ServiceAccountRef.Name,
				Namespace: ard.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     clusterRoleName,
			APIGroup: "rbac.authorization.k8s.io",
		},
	}

	// Check, if CRD is already in place
	logger.Info("fetching CRD")
	crdExists := false
	crd := &apiextensionsv1.CustomResourceDefinition{}
	crdName := fmt.Sprintf("%s.%s", ard.Spec.APIResourceSchemaSpec.Names.Plural, ard.Spec.APIResourceSchemaSpec.Group)

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
			for _, version := range ard.Spec.APIResourceSchemaSpec.Versions {
				gvk := schema.GroupVersionKind{
					Group:   ard.Spec.APIResourceSchemaSpec.Group,
					Version: version.Name,
					Kind:    ard.Spec.APIResourceSchemaSpec.Names.Kind,
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

					logger.Info("there are still resources of CRD left, denying deletion request", "kind:", ard.Spec.APIResourceSchemaSpec.Names.Kind, "list:", resourceNames)
					jsonOutput, err := json.Marshal(resourceNames)
					if err != nil {
						return ctrl.Result{}, errors.Wrap(err, "unable to marshal JSON object")
					}

					r.EventRecorder.Eventf(ard, corev1.EventTypeWarning, "DeletionBlocked",
						"CRD deletion is blocked because dependent resources of kind %s still exist. List of resources: %s", ard.Spec.APIResourceSchemaSpec.Names.Kind, jsonOutput)

					return ctrl.Result{}, nil
				}

				// no resources of the requested kind are found, so we can continue removing the crd
				logger.Info("deleting CRD, as there are no instances of it left", "CRD:", crdName)

				if err := r.Delete(ctx, crd); err != nil {
					return ctrl.Result{}, errors.Wrap(err, "unable to delete CRD")
				}
			}
		}

		// also delete ClusterRole and ClusterRoleBinding, if they exist
		cr := &rbacv1.ClusterRole{}
		if err := deleteIfExists(ctx, r.Client, types.NamespacedName{Name: clusterRoleName}, cr, logger); err != nil {
			return ctrl.Result{}, err
		}

		crb := &rbacv1.ClusterRoleBinding{}
		if err := deleteIfExists(ctx, r.Client, types.NamespacedName{Name: clusterRoleBindingName}, crb, logger); err != nil {
			return ctrl.Result{}, err
		}

		// removing finalizer
		logger.Info("removing finalizer")
		if err := removeFinalizer(ctx, r.Client, ard, finalizerName); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "unable to remove finalizer")
		}

		return ctrl.Result{}, nil
	}

	// resource is not marked for deletion

	// checking, if a finalizer exists on our resource and, if not, add it
	if !controllerutil.ContainsFinalizer(ard, finalizerName) {
		logger.Info("adding finalizer")
		controllerutil.AddFinalizer(ard, finalizerName)
		if err := r.Update(ctx, ard); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "unable to add finalizer to resource")
		}
	}

	// check, if approved APIGroupRequest does exist
	agr := &apiv1alpha1.APIGroupRequest{}
	namespacedName := types.NamespacedName{Namespace: ard.Namespace, Name: ard.Spec.APIResourceSchemaSpec.Group}

	if err := r.Get(ctx, namespacedName, agr); err != nil {
		if apierrors.IsNotFound(err) {
			// if there is no approved APIGroupRequest found, we write status and event and return
			condition := metav1.Condition{
				Type:    "CRDDeployed",
				Status:  "False",
				Reason:  "NoApprovedAPIGroupRequestFound",
				Message: fmt.Sprintf("No APIGroupRequest for API group %s was found, denying request", ard.Spec.APIResourceSchemaSpec.Group),
			}

			logger.Info("no corresponding APIGroupRequest found")
			r.EventRecorder.Eventf(ard, corev1.EventTypeWarning, "RequestDenied", "No APIGroupRequest for API group %s was found, denying request", ard.Spec.APIResourceSchemaSpec.Group)

			if err := updateConditions(ctx, r.Client, req.NamespacedName, ard, &ard.Status.Conditions, condition); err != nil {
				return ctrl.Result{}, errors.Wrap(err, "unable to update status of APIGroupRequest")
			}

			// we return here, as we cannot continue without an approved APIGroupRequest
			return ctrl.Result{}, nil
		}

		// if there was an error while fetching the APIGroupRequest
		return ctrl.Result{}, errors.Wrap(err, "unable to get APIGroupRequest")
	}

	if !agr.Status.Approved {
		// if the APIGroupRequest is not approved, we write status and event and return
		condition := metav1.Condition{
			Type:    "CRDDeployed",
			Status:  "False",
			Reason:  "APIGroupRequestNotApproved",
			Message: fmt.Sprintf("APIGroupRequest for API group %s is not approved, denying request", ard.Spec.APIResourceSchemaSpec.Group),
		}

		logger.Info("APIGroupRequest is not approved")
		r.EventRecorder.Eventf(ard, corev1.EventTypeWarning, "RequestDenied", "APIGroupRequest for API group %s is not approved, denying request", ard.Spec.APIResourceSchemaSpec.Group)

		if err := updateConditions(ctx, r.Client, req.NamespacedName, ard, &ard.Status.Conditions, condition); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "unable to update status of APIGroupRequest")
		}

		return ctrl.Result{}, nil
	}

	// approved APIGroupRequest exists, so we can continue

	// checking for existence of referenced ServiceAccount
	sa := &corev1.ServiceAccount{}
	if err := r.Get(ctx, types.NamespacedName{Name: ard.Spec.ServiceAccountRef.Name, Namespace: ard.Namespace}, sa); err != nil {
		// if ServiceAccount does not exist, we write status and event and return
		if apierrors.IsNotFound(err) {
			condition := metav1.Condition{
				Type:    "CRDDeployed",
				Status:  "False",
				Reason:  "ServiceAccountNotFound",
				Message: fmt.Sprintf("expected ServiceAccount %s not found in namespace %s", ard.Spec.ServiceAccountRef.Name, ard.Namespace),
			}

			logger.Info("ServiceAccount not found")
			r.EventRecorder.Eventf(ard, corev1.EventTypeWarning, "RequestDenied", "ServiceAccount %s not found in namespace %s", ard.Spec.ServiceAccountRef.Name, ard.Namespace)

			if err := updateConditions(ctx, r.Client, req.NamespacedName, ard, &ard.Status.Conditions, condition); err != nil {
				return ctrl.Result{}, errors.Wrap(err, "unable to update status of APIResourceDefinition")
			}

			return ctrl.Result{}, nil
		}

		// if there was an error while fetching the ServiceAccount
		return ctrl.Result{}, errors.Wrap(err, "unable to get ServiceAccount")
	}

	// ServiceAccount exists, so we can continue

	// checking, if Clusterrole and ClusterRoleBinding exist
	cr := &rbacv1.ClusterRole{}

	if err := r.Get(ctx, types.NamespacedName{Name: clusterRoleName}, cr); err != nil {
		if apierrors.IsNotFound(err) {
			// if ClusterRole does not exist, we create it
			logger.Info("creating ClusterRole", "ClusterRole", clusterRoleName)

			if err := r.Create(ctx, clusterRole); err != nil {
				return ctrl.Result{}, errors.Wrap(err, "unable to create ClusterRole")
			}
		} else {
			return ctrl.Result{}, errors.Wrap(err, "unable to get ClusterRole")
		}
	}

	crb := &rbacv1.ClusterRoleBinding{}

	if err := r.Get(ctx, types.NamespacedName{Name: clusterRoleBindingName}, crb); err != nil {
		if apierrors.IsNotFound(err) {
			// if ClusterRoleBinding does not exist, we create it
			logger.Info("creating ClusterRoleBinding", "ClusterRoleBinding", clusterRoleBindingName)

			if err := r.Create(ctx, clusterRoleBinding); err != nil {
				return ctrl.Result{}, errors.Wrap(err, "unable to create ClusterRoleBinding")
			}
		} else {
			return ctrl.Result{}, errors.Wrap(err, "unable to get ClusterRoleBinding")
		}
	}

	// check, if CRD is already in place

	if !crdExists {
		// if CRD does not exist, we create it
		logger.Info("creating CRD", "CRD", crdName)

		crd, err := createCRDfromARD(ard)
		if err != nil {
			return ctrl.Result{}, errors.Wrap(err, "unable to create CRD from APIResourceDefinition")
		}

		if err := r.Create(ctx, crd); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "unable to create CRD")
		}
	}

	// we create condition and event for successful creation of CRD
	condition := metav1.Condition{
		Type:    "CRDDeployed",
		Status:  "True",
		Reason:  "CRDCreated",
		Message: fmt.Sprintf("CRD %s created successfully", crdName),
	}

	if err := updateConditions(ctx, r.Client, req.NamespacedName, ard, &ard.Status.Conditions, condition); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "unable to update status of APIResourceDefinition")
	}

	r.EventRecorder.Eventf(ard, corev1.EventTypeNormal, "CRDCreated", "CRD %s created successfully", crdName)

	return ctrl.Result{}, nil
}

// createCRDfromARD creates a CustomResourceDefinition (CRD) from an APIResourceDefinition (ARD).
// It takes an APIResourceDefinition as input and returns a CustomResourceDefinition and an error.
//
// Parameters:
//   - ard: A pointer to an APIResourceDefinition object.
//
// Returns:
//   - *apiextensionsv1.CustomResourceDefinition: A pointer to the created CustomResourceDefinition object.
//   - error: An error if there is any issue during the creation of the CRD, otherwise nil.
func createCRDfromARD(ard *apiv1alpha1.APIResourceDefinition) (*apiextensionsv1.CustomResourceDefinition, error) {

	crdVersions := []apiextensionsv1.CustomResourceDefinitionVersion{}

	for _, version := range ard.Spec.APIResourceSchemaSpec.Versions {

		validation := &apiextensionsv1.CustomResourceValidation{}
		if err := json.Unmarshal(version.Schema.Raw, validation); err != nil {
			return &apiextensionsv1.CustomResourceDefinition{}, errors.Wrap(err, "could not unmarshal JSONSchemaProps")
		}

		crdVersion := apiextensionsv1.CustomResourceDefinitionVersion{
			Name:                     version.Name,
			Served:                   version.Served,
			Storage:                  version.Storage,
			Deprecated:               version.Deprecated,
			DeprecationWarning:       version.DeprecationWarning,
			Subresources:             &version.Subresources,
			AdditionalPrinterColumns: version.AdditionalPrinterColumns,
			Schema:                   validation,
		}

		crdVersions = append(crdVersions, crdVersion)
	}

	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: ard.Name,
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group:    ard.Spec.APIResourceSchemaSpec.Group,
			Names:    ard.Spec.APIResourceSchemaSpec.Names,
			Scope:    ard.Spec.APIResourceSchemaSpec.Scope,
			Versions: crdVersions,
		},
	}

	return crd, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *APIResourceDefinitionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.EventRecorder = mgr.GetEventRecorderFor("apiresourcedefinition-controller")
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1alpha1.APIResourceDefinition{}).
		Named("apiresourcedefinition").
		Complete(r)
}
