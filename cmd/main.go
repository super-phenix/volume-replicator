package main

import (
	"context"
	"flag"
	"os"
	"os/signal"

	"github.com/skalanetworks/volume-replicator/internal/k8s"
	"github.com/skalanetworks/volume-replicator/internal/replicator"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/klog/v2"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	var kubeconfig, namespace string
	flag.StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig file")
	flag.StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"), "deployment namespace")
	flag.StringVar(&replicator.ExclusionRegex, "exclusion-regex", os.Getenv("EXCLUSION_REGEX"), "regex to exclude PVCs from replication")
	klog.InitFlags(nil)
	flag.Parse()

	if namespace == "" {
		klog.Fatalf("must provide the namespace in which the controller is running through --namespace")
	}

	if err := k8s.Load(kubeconfig); err != nil {
		klog.Fatalf("failed to load kubernetes configuration: %s", err.Error())
	}

	startElection(namespace, ctx)
}

// startElection starts elections among multiple controllers
// The leader starts its internal controller to replicate PVCs, others stay on stand-by
func startElection(namespace string, ctx context.Context) {
	identity, err := os.Hostname()
	if err != nil {
		klog.Fatalf("failed to get hostname: %s", err.Error())
	}

	lock := k8s.GetLease(namespace, identity)
	config := k8s.GetLeaderElectionConfig(lock, startController)

	elector, err := leaderelection.NewLeaderElector(config)
	if err != nil {
		klog.Fatalf("failed to create leader elector: %s", err.Error())
	}
	elector.Run(ctx)
}

// startController starts listening for events and replicating PVCs
func startController(ctx context.Context) {
	controller := replicator.NewController()
	controller.LoadInformers(ctx)
	controller.Run(ctx, 1)
}
