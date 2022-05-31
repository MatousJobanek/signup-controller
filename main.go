/*
Copyright 2022.

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

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/signup-controller/pkg/workspace"
	v1alpha12 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1"
	"github.com/kcp-dev/kcp/pkg/apis/tenancy/v1beta1"
	"go.uber.org/zap/zapcore"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/codeready-toolchain/signup-controller/controllers"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	utilruntime.Must(v1alpha12.AddToScheme(scheme))
	utilruntime.Must(v1beta1.AddToScheme(scheme))

	//+kubebuilder:scaffold:scheme
}

//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="",resources=secrets/finalizers,verbs=update

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	//ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{
		Development: true,
		DestWriter:  os.Stdout,
		Level:       zapcore.DebugLevel,
	})))

	klog.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{
		Development: true,
		DestWriter:  os.Stdout,
		Level:       zapcore.DebugLevel,
	})))

	namespace, err := GetWatchNamespace()
	if err != nil {
		setupLog.Error(err, "Failed to get watch namespace")
		os.Exit(1)
	}
	fmt.Println(namespace)

	cfg := ctrl.GetConfigOrDie()
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		//MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "f208085d.appstudio.kcp.com",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// create client that will be used for retrieving the host operator secret & ToolchainCluster CRs
	cl, err := client.New(cfg, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		setupLog.Error(err, "unable to create a client")
		os.Exit(1)
	}

	orgConfigs, err := workspace.GetWorkspaceConfigs(cl, namespace, client.MatchingLabels{
		"workspace": "org",
	})
	if err != nil {
		setupLog.Error(err, "unable to get org level SA credentials")
		os.Exit(1)
	}
	if len(orgConfigs) == 0 {
		setupLog.Error(nil, "no org level config found")
		os.Exit(1)
	}

	orgCluster, err := cluster.New(orgConfigs[0].Config, func(options *cluster.Options) {
		options.Scheme = scheme
	})
	if err != nil {
		setupLog.Error(err, "unable to create org cluster")
		os.Exit(1)
	}
	if err := mgr.Add(orgCluster); err != nil {
		setupLog.Error(err, "unable to add org cluster")
		os.Exit(1)
	}

	if err = (&controllers.UserSignupReconciler{
		Client:     mgr.GetClient(),
		Scheme:     mgr.GetScheme(),
		OrgCluster: orgCluster,
		Namespace:  namespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterWorkspace")
		os.Exit(1)
	}

	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

const (
	// WatchNamespaceEnvVar is the constant for env variable WATCH_NAMESPACE
	// which is the namespace where the watch activity happens.
	// this value is empty if the operator is running with clusterScope.
	WatchNamespaceEnvVar = "WATCH_NAMESPACE"
)

func GetWatchNamespace() (string, error) {
	ns, found := os.LookupEnv(WatchNamespaceEnvVar)
	if !found {
		return "", fmt.Errorf("%s must be set", WatchNamespaceEnvVar)
	}
	if len(ns) == 0 {
		return "", fmt.Errorf("%s must not be empty", WatchNamespaceEnvVar)
	}
	return ns, nil
}
