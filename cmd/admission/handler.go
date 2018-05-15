package main

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"

	"github.com/golang/glog"

	"k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
)

type admissionHandler struct {
	adm ResourceAdmitter
}

func (h admissionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var review v1beta1.AdmissionReview

	defer func() {
		resp, err := json.Marshal(review)
		if err != nil {
			glog.Errorf("%s: %s", r.RequestURI, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if _, err := w.Write(resp); err != nil {
			glog.Errorf("%s: %s", r.RequestURI, err)
			// TODO(asurikov): make sure we respond with *something* or close the
			// connection.
		}
	}()

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		glog.Errorf("%s: %s", r.RequestURI, err)
		return
	}

	contentType := r.Header.Get("Content-Type")
	const expectType = "application/json"
	if contentType != expectType {
		glog.Errorf("%s: Content-Type is %q, expect %q", r.RequestURI, contentType, expectType)
		return
	}

	deserializer := scheme.Codecs.UniversalDeserializer()

	if _, _, err := deserializer.Decode(body, nil, &review); err != nil {
		glog.Errorf("%s: %s", r.RequestURI, err)
		review.Response = admissionError(err)
		return
	}

	if review.Request == nil {
		glog.Errorf("%s: invalid request", r.RequestURI)
		review.Response = admissionError(errors.New("invalid request"))
		return
	}

	allowed, explanation, err := h.adm.Admit(review.Request)
	if err != nil {
		glog.Errorf("%s: %s", r.RequestURI, err)
		review.Response = admissionError(err)
	} else {
		glog.V(3).Infof("%s: %v", r.RequestURI, allowed)
		review.Response.Allowed = allowed
		// TODO(asurikov): see how Reason and Details are exposed to the user.
		review.Response.Result = &metav1.Status{Message: explanation}
	}
}

func admissionError(err error) *v1beta1.AdmissionResponse {
	return &v1beta1.AdmissionResponse{
		Allowed: *admitFailed,
		Result:  &metav1.Status{Message: err.Error()},
	}
}
