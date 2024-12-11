package controller

import (
	"fmt"

	apiv1alpha1 "github.com/trevex/api-publish-controller/api/v1alpha1"
)

var finalizerName = fmt.Sprintf("%s/finalizer", apiv1alpha1.GroupVersion.Group)
