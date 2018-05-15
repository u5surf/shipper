package rolloutblocks

import (
	"fmt"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	shipperv1 "github.com/bookingcom/shipper/pkg/apis/shipper/v1"
	shipperscheme "github.com/bookingcom/shipper/pkg/client/clientset/versioned/scheme"
	shipperlisters "github.com/bookingcom/shipper/pkg/client/listers/shipper/v1"
)

// Admitter knows how to admit RolloutBlock requests.
type Admitter struct {
	// Lister is a RolloutBlock lister.
	Lister shipperlisters.RolloutBlockLister
}

var gvr = metav1.GroupVersionResource{
	Group:    "shipper.booking.com",
	Version:  "v1",
	Resource: "rolloutblocks",
}

const (
	reasonFailed  = "admission check failed"
	reasonOk      = "admission check worked"
	reasonAlready = "rollouts are already blocked"
)

// Admit decides if a RolloutBlock request is allowed.
// Creating more than one RolloutBlock per namespace is not allowed. Everything
// else is allowed.
func (a Admitter) Admit(req *admissionv1beta1.AdmissionRequest) (bool, string, error) {
	if gvr != req.Resource {
		return false, reasonFailed, fmt.Errorf("expected request for %v but got %v", gvr, req.Resource)
	}

	if req.Operation != admissionv1beta1.Create {
		return true, reasonOk, nil
	}

	deserializer := shipperscheme.Codecs.UniversalDeserializer()

	var rb shipperv1.RolloutBlock
	if _, _, err := deserializer.Decode(req.Object.Raw, nil, &rb); err != nil {
		return false, reasonFailed, err
	}

	existing, err := a.Lister.RolloutBlocks(rb.Namespace).List(labels.Everything())
	if err != nil {
		return false, reasonFailed, err
	}

	if n := len(existing); n > 0 {
		return false, reasonAlready, nil
	}

	return true, reasonOk, nil
}
