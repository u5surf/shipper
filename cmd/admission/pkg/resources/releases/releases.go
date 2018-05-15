package releases

import (
	"fmt"
	"reflect"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bookingcom/shipper/cmd/admission/pkg/check/rolloutsblocked"
	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperscheme "github.com/bookingcom/shipper/pkg/client/clientset/versioned/scheme"
	shipperlisters "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1"
)

// Admitter knows how to admit Release requests.
type Admitter struct {
	// Lister is a RolloutBlock lister.
	Lister shipperlisters.RolloutBlockLister
	// GlobalNs is the designated namespace for RolloutBlocks that apply to all
	// rollouts.
	GlobalNs string
}

var gvr = metav1.GroupVersionResource{
	Group:    "shipper.booking.com",
	Version:  "v1",
	Resource: "applications",
}

const (
	reasonOk      = "admission check worked"
	reasonFailed  = "admission check failed"
	reasonBlocked = "rollouts are blocked, changes to releases are not allowed"
)

// Admit decides if a Release request is allowed.
// Updates to metadata.environment are not allowed.
// Creating more than one Release while a rollout is in progress in not allowed.
// When rollouts are blocked, creation, deletion and spec updates are not
// allowed. If the Release that's being admitted has an override, the request is
// allowed. Adding overrides to existing Releases is allowed. Creating new
// Releases directly with an override is allowed. Deleting existing Releases
// with an override is allowed.
func (a Admitter) Admit(req *admissionv1beta1.AdmissionRequest) (bool, string, error) {
	if gvr != req.Resource {
		return false, reasonFailed, fmt.Errorf("expected request for %v but got %v", gvr, req.Resource)
	}

	deserializer := shipperscheme.Codecs.UniversalDeserializer()

	var rel shipperv1.Release
	if _, _, err := deserializer.Decode(req.Object.Raw, nil, &rel); err != nil {
		return false, reasonFailed, err
	}
	if !rolloutsblocked.Check(rolloutsblocked.Blocks(a.Lister, a.GlobalNs, rel.Namespace), nil, &rel) {
		return true, reasonOk, nil
	}

	if req.Operation == admissionv1beta1.Create || req.Operation == admissionv1beta1.Delete {
		// Delete triggers garbage collection. Don't want.
		return false, reasonBlocked, nil
	}

	// It's an update.

	var oldRel shipperv1.Release
	if _, _, err := deserializer.Decode(req.OldObject.Raw, nil, &oldRel); err != nil {
		return false, reasonFailed, err
	}

	// TODO(asurikov): ensure no changes to metadata.environment.

	if !reflect.DeepEqual(oldRel.Spec, rel.Spec) {
		return false, reasonBlocked, nil
	}

	return true, reasonOk, nil
}
