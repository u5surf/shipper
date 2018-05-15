package main

import (
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
)

// ResourceAdmitter knows how to admit requests for a particular resource.
type ResourceAdmitter interface {
	// Admit decides if a request is allowed. Returns a flag, a human-readable
	// explanation and an error, if one occured.
	Admit(*admissionv1beta1.AdmissionRequest) (bool, string, error)
}
