package controller

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimachinerymeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/onsi/gomega"
	gomegatypes "github.com/onsi/gomega/types"
)

// this function will check via annotations, if a resource is owned
func isOwned(obj client.Object, nn types.NamespacedName, nameAnnotation, namespaceAnnotation string) (ownedByUs, ownedByOthers bool) {
	annotations := obj.GetAnnotations()
	ownedByUs = false
	ownedByOthers = false

	namespacedName := types.NamespacedName{
		Name:      annotations[nameAnnotation],
		Namespace: annotations[namespaceAnnotation],
	}

	_, nameAnnotationExists := annotations[nameAnnotation]
	_, namespaceAnnotationExists := annotations[namespaceAnnotation]

	if nameAnnotationExists && namespaceAnnotationExists {
		if nn == namespacedName {
			ownedByUs = true
		} else {
			ownedByOthers = true
		}
	}

	return
}

func getOwner(resource client.Object) types.NamespacedName {
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

func updateConditions(ctx context.Context, client client.Client, namespacedName types.NamespacedName, obj client.Object, conditions *[]metav1.Condition, condition metav1.Condition) error {
	if err := client.Get(ctx, namespacedName, obj); err != nil {
		return errors.Wrap(err, "unable to get resource")
	}

	apimachinerymeta.SetStatusCondition(conditions, condition)

	if err := client.Status().Update(ctx, obj); err != nil {
		return errors.Wrap(err, "unable to update resource status")
	}

	return nil
}

func deleteIfExists(ctx context.Context, client client.Client, namespacedName types.NamespacedName, obj client.Object, logger logr.Logger) error {

	if err := client.Get(ctx, namespacedName, obj); err != nil {
		if !apierrors.IsNotFound(err) {
			return errors.Wrap(err, "unable to get object")
		}
	} else {
		kind := obj.GetObjectKind().GroupVersionKind().Kind
		logger.Info(fmt.Sprintf("Found corresponding %s, deleting it", kind))

		if err := client.Delete(ctx, obj); err != nil {
			message := fmt.Sprintf("unable to delete %s", kind)
			return errors.Wrap(err, message)
		}
	}
	return nil
}

func removeFinalizer(ctx context.Context, client client.Client, obj client.Object, finalizerName string) error {

	if err := client.Get(ctx, types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, obj); err != nil {
		return errors.Wrap(err, "could not update APIGroupRequest")
	}

	controllerutil.RemoveFinalizer(obj, finalizerName)

	if err := client.Update(ctx, obj); err != nil {
		return errors.Wrap(err, "could not update APIGroupRequest")
	}
	return nil
}

func checkStatus(conditions []metav1.Condition, conditionType, conditionReason string, matcher gomegatypes.GomegaMatcher) {
	apiGroupReservedCondition := apimachinerymeta.FindStatusCondition(conditions, conditionType)
	gomega.Expect(apimachinerymeta.IsStatusConditionTrue(conditions, conditionType)).To(matcher)
	gomega.Expect(apiGroupReservedCondition.Reason).To(gomega.Equal(conditionReason))
}
