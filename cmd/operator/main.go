package main

import (
	"fmt"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	fluxgatev1alpha1 "github.com/north-echo/fluxgate-operator/api/v1alpha1"
	"github.com/north-echo/fluxgate-operator/internal/analyzer"
	"github.com/north-echo/fluxgate-operator/internal/connector"
	"github.com/north-echo/fluxgate-operator/internal/controller"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(fluxgatev1alpha1.AddToScheme(scheme))
}

func main() {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	log := ctrl.Log.WithName("setup")

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
	})
	if err != nil {
		log.Error(err, "unable to create manager")
		fmt.Fprintf(os.Stderr, "unable to create manager: %v\n", err)
		os.Exit(1)
	}

	// Initialize connectors
	connectors := []connector.PipelineConnector{
		&connector.ArgoCDConnector{
			Client: mgr.GetClient(),
			Log:    ctrl.Log.WithName("connectors").WithName("argocd"),
		},
		&connector.FluxConnector{
			Client: mgr.GetClient(),
			Log:    ctrl.Log.WithName("connectors").WithName("flux"),
		},
	}

	// Initialize shared source registry
	registry := controller.NewSourceRegistry()

	// Initialize analyzer
	githubToken := os.Getenv("GITHUB_TOKEN")
	az := analyzer.NewAnalyzer(githubToken, 10*time.Minute)

	// Register controllers
	if err := (&controller.DiscoveryController{
		Client:     mgr.GetClient(),
		Log:        ctrl.Log.WithName("controllers").WithName("Discovery"),
		Scheme:     mgr.GetScheme(),
		Connectors: connectors,
		Registry:   registry,
	}).SetupWithManager(mgr); err != nil {
		log.Error(err, "unable to create controller", "controller", "Discovery")
		os.Exit(1)
	}

	if err := (&controller.AnalysisController{
		Client:     mgr.GetClient(),
		Log:        ctrl.Log.WithName("controllers").WithName("Analysis"),
		Scheme:     mgr.GetScheme(),
		Analyzer:   az,
		Registry:   registry,
		Connectors: connectors,
	}).SetupWithManager(mgr); err != nil {
		log.Error(err, "unable to create controller", "controller", "Analysis")
		os.Exit(1)
	}

	if err := (&controller.ReportController{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("Report"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		log.Error(err, "unable to create controller", "controller", "Report")
		os.Exit(1)
	}

	if err := (&controller.PolicyController{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("Policy"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		log.Error(err, "unable to create controller", "controller", "Policy")
		os.Exit(1)
	}

	log.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Error(err, "problem running manager")
		os.Exit(1)
	}
}
