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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/pkg/errors"
	"github.com/trevex/api-publish-controller/api/v1alpha1"
	apiv1alpha1 "github.com/trevex/api-publish-controller/api/v1alpha1"
)

// APIGroupRequestReconciler reconciles a APIGroupRequest object
type APIGroupRequestReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

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
	_ = log.FromContext(ctx)

	// TODO:
	// Is Request being deleted, delete ClusterAPIGroup AND related API RD!
	agr := &v1alpha1.APIGroupRequest{}
	if err := r.Get(ctx, req.NamespacedName, agr); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, errors.Wrap(err, "Could not get APIGroupRequest")
		}
		return ctrl.Result{}, nil
	}

	cagr := &v1alpha1.ClusterAPIGroup{}
	if err := r.Get(ctx, client.ObjectKey{Name: agr.Name}, cagr); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, errors.Wrap(err, "Could not get ClusterAPIGroup")
		}
		return ctrl.Result{}, nil
	}

	if agr.DeletionTimestamp != nil {

		client.Object.SetFinalizers(agr, []string{})
		if err := r.Delete(ctx, agr); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "Could not delete APIGroupRequest")
		}

		client.Object.SetFinalizers(cagr, []string{})
		if err := r.Delete(ctx, cagr); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "Could not delete ClusterAPIGroup")
		}
	}

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
