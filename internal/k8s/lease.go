package k8s

import (
	"context"
	"github.com/skalanetworks/volume-replicator/internal/constants"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog/v2"
	"os"
	"time"
)

// GetLease returns a Kubernetes lease object
func GetLease(namespace, identity string) resourcelock.Interface {
	return &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      constants.LockName,
			Namespace: namespace,
		},
		Client: ClientSet.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: identity,
		},
	}
}

// GetLeaderElectionConfig returns the election config used to elect leaders
func GetLeaderElectionConfig(lock resourcelock.Interface, startLeading func(ctx context.Context)) leaderelection.LeaderElectionConfig {
	return leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   15 * time.Second,
		RenewDeadline:   10 * time.Second,
		RetryPeriod:     2 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				klog.Info("Became leader, starting controller")
				startLeading(ctx)
			},
			OnStoppedLeading: func() {
				klog.Info("Lost leadership, exiting")
				os.Exit(0)
			},
			OnNewLeader: func(identity string) {
				klog.Infof("Current leader: %s", identity)
			},
		},
	}
}
