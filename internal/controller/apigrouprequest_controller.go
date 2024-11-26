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

var finalizerName = "api.kovi.li/finalizer"

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
	logger.Info("starting reconciliation loop")

	// Is Request being deleted, delete ClusterAPIGroup AND related API RD!
	// Q: Should we really delete all API RD's when removing the APIGroupRequest?
	//		Maybe its better to deny the delete request until the user removes the related API RDs manually to prevent accidental deletion.
	//		Current solution does not include the API RD deletion for now.
	agr := &apiv1alpha1.APIGroupRequest{}
	if err := r.Get(ctx, req.NamespacedName, agr); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, errors.Wrap(err, "Could not get APIGroupRequest")
		}
		return ctrl.Result{}, nil
	}

	annotations := map[string]string{
		"api.kovo.li/request-name":      agr.Name,
		"api.kovo.li/request-namespace": agr.Namespace,
	}
	// not sure if this is needed when setting an OwnerReference, saving for potential later use:
	//
	// controller := true
	// ownerReference := metav1.OwnerReference{
	// 	APIVersion: agr.APIVersion,
	// 	Kind:       agr.Kind,
	// 	Name:       req.Name,
	// 	Controller: &controller,
	// }

	if agr.DeletionTimestamp != nil {

		logger.Info("deleting resource", "APIGroupRequest", agr.Name)

		cagr := &apiv1alpha1.ClusterAPIGroup{}
		if err := r.Get(ctx, client.ObjectKey{Name: agr.Name}, cagr); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, errors.Wrap(err, "Could not get ClusterAPIGroup")
			}
			logger.Info("no ClusterAPIGroup has been found")
			// ClusterAPIGroup has not been found
		} else {
			// ClusterAPIGroup has been found
			owned := hasAnnotations(annotations, agr)

			if owned {
				logger.Info("deleting owned resource", "ClusterAPIGroup", cagr.Name)
				if err := r.Delete(ctx, cagr); err != nil {
					return ctrl.Result{}, errors.Wrap(err, "Could not delete ClusterAPIGroup")
				}
			}
		}

		// Removing finalizer from APIGroupRequest and deleting it
		// Q: Should we check beforehand, if finalizer exists? Current solution expects finalizer to be there.
		logger.Info("removing finalizer", "APIGroupRequest", agr.Name)
		controllerutil.RemoveFinalizer(agr, finalizerName)
		r.Update(ctx, agr)
	}

	// TODO:
	// Does current Request already own an APIGroup, if so check existence.
	// Tries to create ClusterAPIGroup for Request
	// If not exists, create! Make sure Request "OWNS" ClusterAPIGroup
	// If exists and is not owned by current request, set status "invalid"

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *APIGroupRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1alpha1.APIGroupRequest{}).
		Named("apigrouprequest").
		Complete(r)
}
