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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/pkg/errors"
	apiv1alpha1 "github.com/trevex/api-publish-controller/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// APIGroupRequestReconciler reconciles a APIGroupRequest object
type APIGroupRequestReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

var (
	finalizerName              = "api.kovi.li/finalizer"
	requestNameAnnotation      = "api.kovo.li/request-name"
	requestNamespaceAnnotation = "api.kovo.li/request-namespace"
)

// +kubebuilder:rbac:groups=api.kovo.li,resources=apigrouprequests,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=api.kovo.li,resources=apigrouprequests/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=api.kovo.li,resources=apigrouprequests/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the APIGroupRequest object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *APIGroupRequestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.Info("reconciling resource")

	// Is Request being deleted, delete ClusterAPIGroup AND related API RD!
	// Q: Should we really delete all API RD's when removing the APIGroupRequest?
	//		Maybe its better to deny the delete request until the user removes the related API RDs manually to prevent accidental deletion.
	//		Current solution does not include the API RD deletion for now.
	agr := &apiv1alpha1.APIGroupRequest{}
	if err := r.Get(ctx, req.NamespacedName, agr); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, errors.Wrap(err, "could not get APIGroupRequest")
		}
		return ctrl.Result{}, nil
	}

	// checking, if corresponding ClusterAPIGroup exists
	var cagrExists bool
	cagr := &apiv1alpha1.ClusterAPIGroup{}
	if err := r.Get(ctx, client.ObjectKey{Name: agr.Name}, cagr); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, errors.Wrap(err, "could not get ClusterAPIGroup")
		}
		logger.Info("no resource found", "ClusterAPIGroup", cagr.Name)
		// ClusterAPIGroup has not been found
		cagrExists = false
	} else {
		// ClusterAPIGroup has been found
		cagrExists = true
	}

	// Handling resources marked for deletion
	if agr.DeletionTimestamp != nil {

		logger.Info("deleting resource", "APIGroupRequest", agr.Name)
		cagrOwned, _ := isOwned(cagr, req.NamespacedName)

		// deleting ClusterAPIGroup resource, if it exists and is owned by us
		if cagrExists && cagrOwned {
			logger.Info("deleting owned resource", "ClusterAPIGroup", cagr.Name)
			if err := r.Delete(ctx, cagr); err != nil {
				return ctrl.Result{}, errors.Wrap(err, "could not delete ClusterAPIGroup")
			}
		}

		// Removing finalizer from APIGroupRequest and deleting it
		logger.Info("removing finalizer", "APIGroupRequest", agr.Name)
		controllerutil.RemoveFinalizer(agr, finalizerName)
		if err := r.Update(ctx, agr); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "could not update APIGroupRequest")
		}
		return ctrl.Result{}, nil
	}

	// APIGroupRequest is not marked for deletion

	// checking, if a finalizer exists on our resource and, if not, add it
	if !controllerutil.ContainsFinalizer(agr, finalizerName) {
		logger.Info("adding finalizer")
		controllerutil.AddFinalizer(agr, finalizerName)
		if err := r.Update(ctx, agr); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "unable to add finalizer to resource")
		}
	}

	// Now we check, if a corresponding ClusterAPIGroup for our APIGroupRequest already exists.
	if cagrExists {
		_, cagrOwnedByOthers := isOwned(cagr, req.NamespacedName)

		// if it exists and is now owned by our APIGroupRequest
		if cagrOwnedByOthers {
			owner := getClusterApiGroupOwner(cagr)
			logger.Info("resource owned by someone else", "ClusterAPIGroup", cagr.Name, "owner name:", owner.Name, "owner namespace:", owner.Namespace)

			// then we update the status of our APIGroupRequest
			// condition := metav1.Condition{}

		}

	}
	// Does current Request already own an APIGroup, if so check existence.

	// TODO:
	// Tries to create ClusterAPIGroup for Request
	// If not exists, create! Make sure Request "OWNS" ClusterAPIGroup
	// If exists and is not owned by current request, set status "invalid"

	return ctrl.Result{}, nil
}

func getClusterApiGroupOwner(resource *apiv1alpha1.ClusterAPIGroup) types.NamespacedName {
	annotations := resource.GetAnnotations()

	name := annotations[requestNameAnnotation]
	namespace := annotations[requestNamespaceAnnotation]

	if name == "" || namespace == "" {
		return types.NamespacedName{}
	}

	return types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
}

// this function will check via annotations, if a resource is owned
func isOwned(obj client.Object, nn types.NamespacedName) (ownedByUs, ownedByOthers bool) {
	annotations := obj.GetAnnotations()
	ownedByUs = false
	ownedByOthers = false

	namespacedName := types.NamespacedName{
		Name:      annotations[requestNameAnnotation],
		Namespace: annotations[requestNamespaceAnnotation],
	}

	_, reqNameAnnotationExists := annotations[requestNameAnnotation]
	_, reqNamespaceAnnotationExists := annotations[requestNamespaceAnnotation]

	if reqNameAnnotationExists && reqNamespaceAnnotationExists {
		if nn == namespacedName {
			ownedByUs = true
		} else {
			ownedByOthers = true
		}
	}

	return
}

// SetupWithManager sets up the controller with the Manager.
func (r *APIGroupRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1alpha1.APIGroupRequest{}).
		Named("apigrouprequest").
		Complete(r)
}
