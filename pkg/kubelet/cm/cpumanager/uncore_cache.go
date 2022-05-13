package cpumanager

import (
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/klog/v2"
	kubefeatures "k8s.io/kubernetes/pkg/features"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
)

func isUncoreCacheAlignEnabled() bool {
	if utilfeature.DefaultFeatureGate.Enabled(kubefeatures.CPUManagerUncoreCacheAlign) {
		return true
	}
	return false
}

func (a *cpuAccumulator) tryTakeUncoreCacheCPUs(numCpus int) {
	// FIXME must find a single uncore cache for all numCpus, or nothing!
	if numCpus < 2 {
		return // zero or one cpus are already aligned, let the next cpu manager schedule it better
	}

	// FIXME
	if isUncoreCacheAlignEnabled() {
		ucc2cpus := make(map[int]int)
		cpus := a.sortAvailableCPUs()
		for _, cpu := range cpus {
			ucc := a.topo.CPUDetails[cpu].UnCoreCacheID
			ucc2cpus[ucc] += 1
			if ucc2cpus[ucc] == numCpus {
				// take em
				for _, cpu2 := range cpus {
					if a.topo.CPUDetails[cpu2].UnCoreCacheID == ucc {
						a.take(cpuset.NewCPUSet(cpu2))
						numCpus -= 1
						if numCpus == 0 {
							if !a.isSatisfied() {
								klog.Errorf("NOT SATISFIED!!!")
							}
							return
						}
					}
				}
				klog.Errorf("THIS SHOULD NEVER HAPPEN!!!")
				return
			}
		}
	}
}
