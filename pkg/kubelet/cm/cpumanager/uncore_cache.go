package cpumanager

import (
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	kubefeatures "k8s.io/kubernetes/pkg/features"
)

func (a *cpuAccumulator) isUncoreCacheAlignEnabled() bool {
	// FIXME SHOULD BE???
	if utilfeature.DefaultFeatureGate.Enabled(kubefeatures.CPUManagerUncoreCacheAlign) {
		return true
	}
	return false

	// FIXME BUT WAS???
	sauc := a.sortAvailableUncoreCaches()
	l := len(sauc)
	return utilfeature.DefaultFeatureGate.Enabled(kubefeatures.CPUManagerUncoreCacheAlign) &&
		(l != 0 && sauc[0] != sauc[l-1])
	// TODO WHAT IS THIS ABOVE (RANDOM)???
}
