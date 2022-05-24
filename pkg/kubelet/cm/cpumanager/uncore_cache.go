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

func (a *cpuAccumulator) getUnCoreCacheID(cpuid int) int {
	uccID := a.topo.CPUDetails[cpuid].UnCoreCacheID
	return uccID
}

func (a *cpuAccumulator) tryTakeAlignedUncoreCacheCPUs(numCpus int) {
	// must find a single uncore cache for all numCpus, or nothing!
	if !isUncoreCacheAlignEnabled() {
		return // TODO remove this when feature gate is default true... or not
	}
	if numCpus <= 1 {
		return // zero?? or one cpu is already aligned, let the next cpu manager allocate it better
	}
	// The other cpu managers use this to make decisions about allocations
	// If we need to change the sorting algorithm, we should do it locally here (not everywhere)
	cpus := a.sortAvailableCPUs()
	// Count how many cpus are in each uncore cache
	ucc2count := make(map[int]int)
	for _, cpu := range cpus {
		ucc := a.getUnCoreCacheID(cpu)
		// use this to get the len()
		ucc2count[ucc] += 1
	}
	if len(ucc2count) <= 1 {
		return // all remaining free cpus have same uncore cache, let the next cpu manager allocate it better
	}
	// find ucc that has enough cpus
	for ucc, count := range ucc2count {
		if count < numCpus {
			continue // not enough cpus in this ucc
		}
		// take cpus from this ucc
		for _, cpu := range cpus {
			if a.getUnCoreCacheID(cpu) != ucc {
				continue // only taking cpus in ucc
			}
			a.take(cpuset.NewCPUSet(cpu))
			numCpus -= 1
			if numCpus == 0 {
				if !a.isSatisfied() {
					klog.Errorf("NOT SATISFIED!!!")
				}
				return // SUCCESS
			}
		}
		klog.Errorf("THIS SHOULD NEVER HAPPEN!!!")
		return
	}
	return // NO CHOICE
}

// EVERYTHING BELOW THIS LINE IS POTENTIALLY FOSSIL CODE

// from cpu_assignment.go
func (a *cpuAccumulator) PROPOSED_sortAvailableCores() []int {
	if isUncoreCacheAlignEnabled() {
		var result []int
		for _, cache := range a.sortAvailableUncoreCaches() {
			cores := a.details.CoresInUncoreCaches(cache).ToSliceNoSort()
			a.sort(cores, a.details.CPUsInCores)
			result = append(result, cores...)
		}
		return result
	}
	return a.numaOrSocketsFirst.sortAvailableCores()
}

// Returns true if the supplied core is fully available in `topoDetails`.
// func (a *cpuAccumulator) isUncoreCacheFree(uncoreCacheID int) bool {
// 	return a.details.CPUsInUncoreCaches(uncoreCacheID).Size() == a.topo.CPUsPerUncoreCache()
// }

// Returns free uncore cache IDs as a slice sorted by sortAvailableUncoreCaches().
// Only support when CpuManagerUncoreCacheAlign is enabled.
// func (a *cpuAccumulator) freeUncoreCaches() []int {
// 	free := []int{}
// 	for _, cache := range a.sortAvailableUncoreCaches() {
// 		if a.isUncoreCacheFree(cache) {
// 			free = append(free, cache)
// 		}
// 	}
// 	return free
// }

// Sort all sockets with free CPUs using the sort() algorithm defined above.
func (a *cpuAccumulator) sortAvailableUncoreCaches() []int {
	var result []int
	for _, socket := range a.sortAvailableSockets() {
		caches := a.details.UncoreCachesInSocket(socket).ToSliceNoSort()
		a.sort(caches, a.details.CPUsInUncoreCaches)
		result = append(result, caches...)
	}
	return result
}

func (a *cpuAccumulator) takeFullUncoreCaches() {
	//	for _, uncorecache := range a.freeUncoreCaches() {
	//		cpusInUncoreCache := a.topo.CPUDetails.CPUsInUncoreCaches(uncorecache)
	//		if !a.needs(cpusInUncoreCache.Size()) {
	//			continue
	//		}
	//		klog.V(4).InfoS("takeFullUncoreCaches: claiming uncore-cache", "uncore-cache", uncorecache)
	//		a.take(cpusInUncoreCache)
	//	}
}

// 2. Acquire whole uncore cache, if available and the container requires at least
//    a uncore-cache's-worth of CPUs.
//    Only support when CpuManagerUncoreCacheAlign is enabled.
// if isUncoreCacheAlignEnabled() {
// acc.takeFullUncoreCaches()
// if acc.isSatisfied() {
// return acc.result, nil
// }
// }

// 3. Acquire whole cores, if available and the container requires at least
