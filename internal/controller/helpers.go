package controller

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// These functions are copied from the main branch of github.com/kubernetes-sigs/controller-runtime, which already has the function "HasOwnerReference".
// As soon as a new release including that function is available, this file can be removed and the function from controller-runtime can be used

func HasOwnerReference(ownerRefs []metav1.OwnerReference, obj client.Object, scheme *runtime.Scheme) (bool, error) {
	gvk, err := apiutil.GVKForObject(obj, scheme)
	if err != nil {
		return false, err
	}
	idx := indexOwnerRef(ownerRefs, metav1.OwnerReference{
		APIVersion: gvk.GroupVersion().String(),
		Name:       obj.GetName(),
		Kind:       gvk.Kind,
	})
	return idx != -1, nil
}

func indexOwnerRef(ownerReferences []metav1.OwnerReference, ref metav1.OwnerReference) int {
	for index, r := range ownerReferences {
		if referSameObject(r, ref) {
			return index
		}
	}
	return -1
}

func referSameObject(a, b metav1.OwnerReference) bool {
	aGV, err := schema.ParseGroupVersion(a.APIVersion)
	if err != nil {
		return false
	}

	bGV, err := schema.ParseGroupVersion(b.APIVersion)
	if err != nil {
		return false
	}
	return aGV.Group == bGV.Group && a.Kind == b.Kind && a.Name == b.Name
}

// function for checking, if resource has a certain set of annotations

func hasAnnotations(annotations map[string]string, obj client.Object) bool {
	objAnnotations := obj.GetAnnotations()
	for k, v := range annotations {
		if objAnnotations[k] == v {
			continue
		} else {
			return false
		}
	}
	return true
}
