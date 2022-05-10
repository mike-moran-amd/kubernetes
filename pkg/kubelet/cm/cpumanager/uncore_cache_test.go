package cpumanager

import (
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/features"
	"reflect"
	"testing"

	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager/topology"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
)

// my IDE is auto generating the above lines ^^^

func cpusetForCPUTopology(topo *topology.CPUTopology) cpuset.CPUSet {
	// Many of the test cases use cpuset.CPUSet based on the topology
	elems := makeRange(0, topo.NumCPUs)
	result := cpuset.NewCPUSet(elems...)
	// FIXME return all of them, --> filtered and masked
	return result
}

func makeRange(min, max int) []int {
	// from Stack Overflow:
	// There is no equivalent to PHP's range in the Go standard library.
	// You have to create one yourself.                               <-- NEAT!
	// The simplest is to use a for loop:
	a := make([]int, max-min+1)
	for i := range a {
		a[i] = min + i
	}
	return a
}

func TestNewCPUSet(t *testing.T) {
	// I was
	testCases := []struct {
		description   string
		availableCPUs cpuset.CPUSet
		expect        []int
	}{
		{
			"CAN BE EMPTY",
			cpuset.NewCPUSet(),
			[]int{},
		},
		{
			"CAN HAVE ONE ELEMENT",
			cpuset.NewCPUSet(0),
			[]int{0},
		},
		{
			"CAN HAVE NEGATIVE ELEMENTS",
			cpuset.NewCPUSet(-1),
			[]int{-1},
		},
		{
			"ARE ORDERED IF USING ToSliceNoSort()",
			cpuset.NewCPUSet(0, -1),
			[]int{0, -1},
		},
	}
	// THIS IS THE MULTI TEST PATTERN
	for _, tc := range testCases {
		// EACH TEST CASE
		t.Run(tc.description, func(t *testing.T) {
			// DO THE TEST
			result := tc.availableCPUs.ToSliceNoSort()
			// CHECK RESULT
			if !reflect.DeepEqual(result, tc.expect) {
				// RAISE ERROR
				t.Errorf("expected %v to equal %v", result, tc.expect)
			}
		})
	}
}

func TestFreeSockets(t *testing.T) {
	testCases := []struct {
		description       string
		topo              *topology.CPUTopology
		expectFreeSockets []int
	}{
		{
			"single socket HT, 1 socket free",
			topoSingleSocketHT,
			[]int{0},
		},
		{
			"dual socket HT, 2 sockets free",
			topoDualSocketHT,
			[]int{0, 1},
		},
		{
			"dual socket no HT",
			topoDualSocketNoHT,
			[]int{0, 1},
		},
		{
			"triple socket HT",
			topoTripleSocketHT,
			[]int{0, 1, 2},
		},
		{
			"dual socket, multi numa per socket, HT, 2 sockets free",
			topoDualSocketMultiNumaPerSocketHT,
			[]int{0, 1},
		},
		{
			"dual numa, multi socket per per socket, HT, 4 sockets free",
			fakeTopoMultiSocketDualSocketPerNumaHT,
			[]int{0, 1, 2, 3},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			acc := newCPUAccumulator(tc.topo, cpusetForCPUTopology(tc.topo), 0)
			freeSockets := acc.freeSockets()
			if !reflect.DeepEqual(freeSockets, tc.expectFreeSockets) {
				t.Errorf("expected %v to equal %v", freeSockets, tc.expectFreeSockets)
			}
		})
	}
}

func Test_newCPUAccumulator_freeCores(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.CPUManagerUncoreCacheAlign, true)()
	topo := topoDualSocketHT
	cpuSet := cpuset.NewCPUSet(0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11)
	expectedResult := []int{0, 2, 4, 1, 3, 5}

	acc := newCPUAccumulator(topo, cpuSet, 0)
	result := acc.freeCores()

	if !reflect.DeepEqual(result, expectedResult) {
		t.Errorf("expected %v to equal %v", result, expectedResult)
	}
}

// from cpu_assignment.go
func (a *cpuAccumulator) PROPOSED_sortAvailableCores() []int {
	if a.isUncoreCacheAlignEnabled() {
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
func (a *cpuAccumulator) isUncoreCacheFree(uncoreCacheID int) bool {
	return a.details.CPUsInUncoreCaches(uncoreCacheID).Size() == a.topo.CPUsPerUncoreCache()
}

// Returns free uncore cache IDs as a slice sorted by sortAvailableUncoreCaches().
// Only support when CpuManagerUncoreCacheAlign is enabled.
func (a *cpuAccumulator) freeUncoreCaches() []int {
	free := []int{}
	for _, cache := range a.sortAvailableUncoreCaches() {
		if a.isUncoreCacheFree(cache) {
			free = append(free, cache)
		}
	}
	return free
}

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
	for _, uncorecache := range a.freeUncoreCaches() {
		cpusInUncoreCache := a.topo.CPUDetails.CPUsInUncoreCaches(uncorecache)
		if !a.needs(cpusInUncoreCache.Size()) {
			continue
		}
		klog.V(4).InfoS("takeFullUncoreCaches: claiming uncore-cache", "uncore-cache", uncorecache)
		a.take(cpusInUncoreCache)
	}
}

// 2. Acquire whole uncore cache, if available and the container requires at least
//    a uncore-cache's-worth of CPUs.
//    Only support when CpuManagerUncoreCacheAlign is enabled.
// if acc.isUncoreCacheAlignEnabled() {
// acc.takeFullUncoreCaches()
// if acc.isSatisfied() {
// return acc.result, nil
// }
// }

// 3. Acquire whole cores, if available and the container requires at least

func TestCPUAccumulatorFreeCoresUncoreCacheEnabled(t *testing.T) {
	testCases := []struct {
		description   string
		topo          *topology.CPUTopology
		availableCPUs cpuset.CPUSet
		expect        []int
	}{
		{
			"single socket HT, 4 cores free",
			topoSingleSocketHT,
			cpuset.NewCPUSet(0, 1, 2, 3, 4, 5, 6, 7),
			[]int{0, 1, 2, 3},
		},
		{
			"single socket HT, 3 cores free",
			topoSingleSocketHT,
			cpuset.NewCPUSet(0, 1, 2, 4, 5, 6),
			[]int{0, 1, 2},
		},
		{
			"single socket HT, 3 cores free (1 partially consumed)",
			topoSingleSocketHT,
			cpuset.NewCPUSet(0, 1, 2, 3, 4, 5, 6),
			[]int{0, 1, 2},
		},
		{
			"single socket HT, 0 cores free",
			topoSingleSocketHT,
			cpuset.NewCPUSet(),
			[]int{},
		},
		{
			"single socket HT, 0 cores free (4 partially consumed)",
			topoSingleSocketHT,
			cpuset.NewCPUSet(0, 1, 2, 3),
			[]int{},
		},
		{
			"dual socket HT, 6 cores free",
			topoDualSocketHT,
			cpuset.NewCPUSet(0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11),
			[]int{0, 2, 4, 1, 3, 5},
		},
		{
			"dual socket HT, 5 cores free (1 consumed from socket 0)",
			topoDualSocketHT,
			cpuset.NewCPUSet(2, 1, 3, 4, 5, 7, 8, 9, 10, 11),
			[]int{2, 4, 1, 3, 5},
		},
		{
			"dual socket HT, 5 cores free (1 consumed from socket 1)",
			topoDualSocketHT,
			cpuset.NewCPUSet(0, 1, 2, 3, 4, 6, 7, 8, 9, 10),
			[]int{1, 3, 0, 2, 4},
		},
		{
			"dual socket HT, 4 cores free (1 consumed from each socket)",
			topoDualSocketHT,
			cpuset.NewCPUSet(2, 3, 4, 5, 8, 9, 10, 11),
			[]int{2, 4, 3, 5},
		},
	}
	defer featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.CPUManagerUncoreCacheAlign, true)()
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			acc := newCPUAccumulator(tc.topo, tc.availableCPUs, 0)
			result := acc.freeCores()
			if !reflect.DeepEqual(result, tc.expect) {
				t.Errorf("[%s] expected %v to equal %v", tc.description, result, tc.expect)
			}
		})
	}
}

func TestCPUAccumulatorFreeCPUsUncoreCacheEnabled(t *testing.T) {
	testCases := []struct {
		description   string
		topo          *topology.CPUTopology
		availableCPUs cpuset.CPUSet
		expect        []int
	}{
		{
			"single socket HT, 8 cpus free",
			topoSingleSocketHT,
			cpuset.NewCPUSet(0, 1, 2, 3, 4, 5, 6, 7),
			[]int{0, 4, 1, 5, 2, 6, 3, 7},
		},
		{
			"single socket HT, 5 cpus free",
			topoSingleSocketHT,
			cpuset.NewCPUSet(3, 4, 5, 6, 7),
			[]int{4, 5, 6, 3, 7},
		},
		{
			"dual socket HT, 12 cpus free",
			topoDualSocketHT,
			cpuset.NewCPUSet(0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11),
			[]int{0, 6, 2, 8, 4, 10, 1, 7, 3, 9, 5, 11},
		},
		{
			"dual socket HT, 11 cpus free",
			topoDualSocketHT,
			cpuset.NewCPUSet(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11),
			[]int{6, 2, 8, 4, 10, 1, 7, 3, 9, 5, 11},
		},
		{
			"dual socket HT, 10 cpus free",
			topoDualSocketHT,
			cpuset.NewCPUSet(1, 2, 3, 4, 5, 7, 8, 9, 10, 11),
			[]int{2, 8, 4, 10, 1, 7, 3, 9, 5, 11},
		},
		{
			"dual socket HT, 10 cpus free",
			topoDualSocketHT,
			cpuset.NewCPUSet(0, 1, 2, 3, 4, 6, 7, 8, 9, 10),
			[]int{1, 7, 3, 9, 0, 6, 2, 8, 4, 10},
		},
	}
	defer featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.CPUManagerUncoreCacheAlign, true)()
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			acc := newCPUAccumulator(tc.topo, tc.availableCPUs, 0)
			result := acc.freeCores()
			if !reflect.DeepEqual(result, tc.expect) {
				t.Errorf("[%s] expected %v to equal %v", tc.description, result, tc.expect)
			}
		})
	}
}

func TestTakeByTopologyUncoreCacheEnabled(t *testing.T) {
	testCases := []struct {
		description   string
		topo          *topology.CPUTopology
		availableCPUs cpuset.CPUSet
		numCPUs       int
		expErr        string
		expResult     cpuset.CPUSet
	}{
		{
			"take more cpus than are available from single socket with HT",
			topoSingleSocketHT,
			cpuset.NewCPUSet(0, 2, 4, 6),
			5,
			"not enough cpus available to satisfy request",
			cpuset.NewCPUSet(),
		},
		{
			"take zero cpus from single socket with HT",
			topoSingleSocketHT,
			cpuset.NewCPUSet(0, 1, 2, 3, 4, 5, 6, 7),
			0,
			"",
			cpuset.NewCPUSet(),
		},
		{
			"take one cpu from single socket with HT",
			topoSingleSocketHT,
			cpuset.NewCPUSet(0, 1, 2, 3, 4, 5, 6, 7),
			1,
			"",
			cpuset.NewCPUSet(0),
		},
		{
			"take one cpu from single socket with HT, some cpus are taken",
			topoSingleSocketHT,
			cpuset.NewCPUSet(1, 3, 5, 6, 7),
			1,
			"",
			cpuset.NewCPUSet(6),
		},
		{
			"take two cpus from single socket with HT",
			topoSingleSocketHT,
			cpuset.NewCPUSet(0, 1, 2, 3, 4, 5, 6, 7),
			2,
			"",
			cpuset.NewCPUSet(0, 4),
		},
		{
			"take all cpus from single socket with HT",
			topoSingleSocketHT,
			cpuset.NewCPUSet(0, 1, 2, 3, 4, 5, 6, 7),
			8,
			"",
			cpuset.NewCPUSet(0, 1, 2, 3, 4, 5, 6, 7),
		},
		{
			"take two cpus from single socket with HT, only one core totally free",
			topoSingleSocketHT,
			cpuset.NewCPUSet(0, 1, 2, 3, 6),
			2,
			"",
			cpuset.NewCPUSet(2, 6),
		},
		{
			"take one cpu from dual socket with HT - core from Socket 0",
			topoDualSocketHT,
			cpuset.NewCPUSet(1, 2, 3, 4, 5, 7, 8, 9, 10, 11),
			1,
			"",
			cpuset.NewCPUSet(2),
		},
		{
			"take a socket of cpus from dual socket with HT",
			topoDualSocketHT,
			cpuset.NewCPUSet(0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11),
			6,
			"",
			cpuset.NewCPUSet(0, 2, 4, 6, 8, 10),
		},
	}

	defer featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.CPUManagerUncoreCacheAlign, true)()
	for _, tc := range testCases {
		// Apply t.Run() test pattern
		t.Run(tc.description, func(t *testing.T) {
			result, err := takeByTopologyNUMAPacked(tc.topo, tc.availableCPUs, tc.numCPUs)
			if tc.expErr != "" && err.Error() != tc.expErr {
				t.Errorf("expected error to be [%v] but it was [%v] in test \"%s\"", tc.expErr, err, tc.description)
			}
			if !result.Equals(tc.expResult) {
				t.Errorf("expected result [%s] to equal [%s] in test \"%s\"", result, tc.expResult, tc.description)
			}
		})
	}
}

var (
	topoDualUncoreCacheSingleSocketHT = &topology.CPUTopology{
		NumCPUs:         16,
		NumSockets:      1,
		NumCores:        8,
		NumUnCoreCaches: 2,
		CPUDetails: map[int]topology.CPUInfo{
			0:  {CoreID: 0, SocketID: 0, UnCoreCacheID: 0},
			1:  {CoreID: 0, SocketID: 0, UnCoreCacheID: 0},
			2:  {CoreID: 1, SocketID: 0, UnCoreCacheID: 0},
			3:  {CoreID: 1, SocketID: 0, UnCoreCacheID: 0},
			4:  {CoreID: 2, SocketID: 0, UnCoreCacheID: 0},
			5:  {CoreID: 2, SocketID: 0, UnCoreCacheID: 0},
			6:  {CoreID: 3, SocketID: 0, UnCoreCacheID: 0},
			7:  {CoreID: 3, SocketID: 0, UnCoreCacheID: 0},
			8:  {CoreID: 4, SocketID: 0, UnCoreCacheID: 1},
			9:  {CoreID: 4, SocketID: 0, UnCoreCacheID: 1},
			10: {CoreID: 5, SocketID: 0, UnCoreCacheID: 1},
			11: {CoreID: 5, SocketID: 0, UnCoreCacheID: 1},
			12: {CoreID: 6, SocketID: 0, UnCoreCacheID: 1},
			13: {CoreID: 6, SocketID: 0, UnCoreCacheID: 1},
			14: {CoreID: 7, SocketID: 0, UnCoreCacheID: 1},
			15: {CoreID: 7, SocketID: 0, UnCoreCacheID: 1},
		},
	}
	// FIXME comment from jfbai: topoDualUncoreCacheSingleSocketHT = &topology.CPUTopology{ NumCPUs: 12, NumSockets: 2, NumCores: 6, NumUnCoreCaches: 4, CPUDetails: map[int]topology.CPUInfo{ 0: {CoreID: 0, SocketID: 0, UnCoreCacheID: 0}, 1: {CoreID: 0, SocketID: 0, UnCoreCacheID: 0}, 2: {CoreID: 1, SocketID: 0, UnCoreCacheID: 0}, 3: {CoreID: 1, SocketID: 0, UnCoreCacheID: 0}, 4: {CoreID: 2, SocketID: 0, UnCoreCacheID: 1}, 5: {CoreID: 2, SocketID: 0, UnCoreCacheID: 1}, 6: {CoreID: 3, SocketID: 1, UnCoreCacheID: 8}, 7: {CoreID: 3, SocketID: 1, UnCoreCacheID: 8}, 8: {CoreID: 4, SocketID: 1, UnCoreCacheID: 8}, 9: {CoreID: 4, SocketID: 1, UnCoreCacheID: 8}, 10: {CoreID: 5, SocketID: 1, UnCoreCacheID: 9}, 11: {CoreID: 5, SocketID: 1, UnCoreCacheID: 9}, }, }
)

func TestCPUAccumulatorFreeUncoreCache(t *testing.T) {
	testCases := []struct {
		description   string
		topo          *topology.CPUTopology
		availableCPUs cpuset.CPUSet
		expect        []int
	}{
		{
			"dual UncoreCache groups, 1 uncore cache free, cache id and cpu numbers (0:7, 1:8)",
			topoDualUncoreCacheSingleSocketHT,
			cpuset.NewCPUSet(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15),
			[]int{1},
		},
		{
			"dual UncoreCache groups, 2 uncore cache free, cache id and cpu numbers (0:8, 1:8)",
			topoDualUncoreCacheSingleSocketHT,
			cpuset.NewCPUSet(0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15),
			[]int{0, 1},
		},
		{
			"dual UncoreCache groups, 1 uncore cache free, cache id and cpu numbers (0:8, 1:7)",
			topoDualUncoreCacheSingleSocketHT,
			cpuset.NewCPUSet(0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14),
			[]int{0},
		},
		{
			"dual UncoreCache groups, 0 uncore cache free, cache id and cpu numbers (0:7, 1:7)",
			topoDualUncoreCacheSingleSocketHT,
			cpuset.NewCPUSet(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14),
			[]int{},
		},
	}
	for _, tc := range testCases {
		// Apply t.Run() test pattern
		t.Run(tc.description, func(t *testing.T) {
			acc := newCPUAccumulator(tc.topo, tc.availableCPUs, 0)
			result := acc.freeUncoreCaches()
			if !reflect.DeepEqual(result, tc.expect) {
				t.Errorf("[%s] expected %v to equal %v", tc.description, result, tc.expect)
			}
		})
	}
}
