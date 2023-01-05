package features

import (
	"k8s.io/component-base/featuregate"
	"k8s.io/klog/v2"
)

const (
	ManagedServiceAccountEphemeralIdentity featuregate.Feature = "ManagedServiceAccountEphemeralIdentity"
)

var FeatureGates featuregate.MutableFeatureGate = featuregate.NewFeatureGate()

func init() {
	if err := FeatureGates.Add(DefaultFeatureGates); err != nil {
		klog.Fatalf("Unexpected error: %v", err)
	}
}

// DefaultFeatureGates consists of all known specific feature keys.
// To add a new feature, define a key for it above and add it here.
var DefaultFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	ManagedServiceAccountEphemeralIdentity: {Default: false, PreRelease: featuregate.Alpha},
}
