package main

import (
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/golang/glog"

	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/bookingcom/shipper/cmd/admission/pkg/resources/applications"
	"github.com/bookingcom/shipper/cmd/admission/pkg/resources/releases"
	"github.com/bookingcom/shipper/cmd/admission/pkg/resources/rolloutblocks"
	shipperclientset "github.com/bookingcom/shipper/pkg/client/clientset/versioned"
	shipperinformers "github.com/bookingcom/shipper/pkg/client/informers/externalversions"
)

var (
	masterURL    = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig   = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	certPath     = flag.String("cert", "", "Path to the apiserver TLS certificate.")
	keyPath      = flag.String("key", "", "Path to the apiserver TLS private key.")
	resyncPeriod = flag.String("resync", "5m", "Informer's cache re-sync in Go's duration format.")
	addr         = flag.String("addr", ":8891", "Addr to expose webhooks on.")
	shipperNs    = flag.String("shipper-namespace", "shipper-system", "Namespace for Shipper resources.")

	// Playing it safe here. If we weren't able to admit a request, forbid it by default.
	admitFailed = flag.Bool("admit-failed", false, "Whether to admit requests for which the admission check failed.")
)

func main() {
	flag.Parse()

	glog.Infof("Starting admission-webhook on %s", *addr)
	defer glog.Info("Stopping admission-webhook")

	restCfg, err := clientcmd.BuildConfigFromFlags(*masterURL, *kubeconfig)
	if err != nil {
		glog.Fatal(err)
	}

	shipperClient, err := shipperclientset.NewForConfig(restCfg)
	if err != nil {
		glog.Fatal(err)
	}

	kubeClient, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		glog.Fatal(err)
	}

	resync, err := time.ParseDuration(*resyncPeriod)
	if err != nil {
		glog.Warningf("Couldn't parse resync period %q, defaulting to 5 minutes", *resyncPeriod)
		resync = 5 * time.Minute
	}

	stopCh := setupSignalHandler()

	shipperInformerFactory := shipperinformers.NewSharedInformerFactory(shipperClient, resync)
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, resync)

	shipperInformerFactory.Start(stopCh)
	kubeInformerFactory.Start(stopCh)

	shipperInformerFactory.WaitForCacheSync(stopCh)
	kubeInformerFactory.WaitForCacheSync(stopCh)

	go func() {
		rbLister := shipperInformerFactory.Shipper().V1().RolloutBlocks().Lister()

		mux := http.NewServeMux()
		for _, v := range []struct {
			ep  string
			adm ResourceAdmitter
		}{
			{"/applications", applications.Admitter{rbLister, *shipperNs}},
			{"/releases", releases.Admitter{rbLister, *shipperNs}},
			{"/rolloutblocks", rolloutblocks.Admitter{rbLister}},
		} {
			mux.Handle(v.ep, admissionHandler{v.adm})
		}

		srv := http.Server{
			Addr:    *addr,
			Handler: mux,
			//TLSConfig: &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert}, // mTLS
		}
		glog.Fatal(srv.ListenAndServeTLS(*certPath, *keyPath))
	}()

	<-stopCh
}

func setupSignalHandler() <-chan struct{} {
	stopCh := make(chan struct{})

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigCh
		close(stopCh)
		<-sigCh
		os.Exit(1) // Second signal. Exit directly.
	}()

	return stopCh
}
