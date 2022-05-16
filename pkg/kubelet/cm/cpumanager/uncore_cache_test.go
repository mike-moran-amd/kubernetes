package cpumanager

import (
	"fmt"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"k8s.io/kubernetes/pkg/features"
	"reflect"
	"testing"

	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager/topology"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
)

func cpusetForCPUTopology(topo *topology.CPUTopology) cpuset.CPUSet {
	// Many of the test cases use cpuset.CPUSet based on the topology
	elems := makeRange(0, topo.NumCPUs)
	result := cpuset.NewCPUSet(elems...)
	// TODO filters and masks?
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

func TestCPUAccumulatorFreeCPUsUncoreCacheEnabledLegacy(t *testing.T) {
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
			result := acc.freeCPUs()
			if !reflect.DeepEqual(result, tc.expect) {
				t.Errorf("[%s] expected %v to equal %v", tc.description, result, tc.expect)
			}
		})
	}
}

func TestTakeByTopologyUncoreCacheEnabledLegacy(t *testing.T) {
	testCases := []struct {
		description   string
		topo          *topology.CPUTopology
		availableCPUs cpuset.CPUSet
		numCPUs       int
		expErr        string
		expResult     cpuset.CPUSet
	}{
		// None of the topologies in this test should have more than one uncore cache
		// e.g. the old tests should not change
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
				t.Errorf("[%s] expected %v to equal %v", tc.description, err, tc.expErr)
			}
			if !result.Equals(tc.expResult) {
				t.Errorf("[%s] expected %v to equal %v", tc.description, result, tc.expResult)
			}
		})
	}
}

var (
	topoDualUncoreCacheSingleSocketHT = &topology.CPUTopology{
		NumCPUs:    16,
		NumSockets: 1,
		NumCores:   8,
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

	// FIXME comment from jfbai: topoDualUncoreCacheSingleSocketHT = &topology.CPUTopology{
	// NumCPUs: 12,
	// NumSockets: 2,
	// NumCores: 6,
	// NumUnCoreCaches: 4,
	// CPUDetails: map[int]topology.CPUInfo{
	//  0: {CoreID: 0, SocketID: 0, UnCoreCacheID: 0},
	//  1: {CoreID: 0, SocketID: 0, UnCoreCacheID: 0},
	//  2: {CoreID: 1, SocketID: 0, UnCoreCacheID: 0},
	//  3: {CoreID: 1, SocketID: 0, UnCoreCacheID: 0},
	//  4: {CoreID: 2, SocketID: 0, UnCoreCacheID: 1},
	//  5: {CoreID: 2, SocketID: 0, UnCoreCacheID: 1},
	//  6: {CoreID: 3, SocketID: 1, UnCoreCacheID: 8},
	//  7: {CoreID: 3, SocketID: 1, UnCoreCacheID: 8},
	//  8: {CoreID: 4, SocketID: 1, UnCoreCacheID: 8},
	//  9: {CoreID: 4, SocketID: 1, UnCoreCacheID: 8},
	// 10: {CoreID: 5, SocketID: 1, UnCoreCacheID: 9},
	// 11: {CoreID: 5, SocketID: 1, UnCoreCacheID: 9}, }, }

	topoFROMjfbai = &topology.CPUTopology{
		NumCPUs:    12,
		NumSockets: 2,
		NumCores:   6,
		CPUDetails: map[int]topology.CPUInfo{
			0:  {CoreID: 0, SocketID: 0, UnCoreCacheID: 0},
			1:  {CoreID: 0, SocketID: 0, UnCoreCacheID: 0},
			2:  {CoreID: 1, SocketID: 0, UnCoreCacheID: 0},
			3:  {CoreID: 1, SocketID: 0, UnCoreCacheID: 0},
			4:  {CoreID: 2, SocketID: 0, UnCoreCacheID: 1},
			5:  {CoreID: 2, SocketID: 0, UnCoreCacheID: 1},
			6:  {CoreID: 3, SocketID: 1, UnCoreCacheID: 8},
			7:  {CoreID: 3, SocketID: 1, UnCoreCacheID: 8},
			8:  {CoreID: 4, SocketID: 1, UnCoreCacheID: 8},
			9:  {CoreID: 4, SocketID: 1, UnCoreCacheID: 8},
			10: {CoreID: 5, SocketID: 1, UnCoreCacheID: 9},
			11: {CoreID: 5, SocketID: 1, UnCoreCacheID: 9},
		},
	}
)

func TestTakeTopology(t *testing.T) {
	testCases := []struct {
		description string
		topo        *topology.CPUTopology
		numCpus     int
		expResult   string
	}{
		{
			"topoDualUncoreCacheSingleSocketHT",
			topoDualUncoreCacheSingleSocketHT,
			3, // 2 is non-deterministic
			"map[1:0-2 2:3-5 3:8-10 4:11-13 5:6-7,14]",
		},
		{
			"topoFROMjfbai",
			topoFROMjfbai,
			3, // 2 is non-deterministic
			"map[1:0-2 2:6-8 3:3-5 4:9-11]",
		},
	}

	defer featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.CPUManagerUncoreCacheAlign, true)()
	for _, tc := range testCases {
		t.Run(tc.description+fmt.Sprintf("_BY_%d_numCpus", tc.numCpus), func(t *testing.T) {
			results := make(map[int]cpuset.CPUSet)
			cpuSet := cpusetForCPUTopology(tc.topo)
			counter := 0
			for len(cpuSet.ToSliceNoSort()) >= tc.numCpus {
				took, err := takeByTopologyNUMAPacked(tc.topo, cpuSet, tc.numCpus)
				if err != nil {
					t.Errorf("[%s] ERROR: %v", tc.description, err.Error())
					break
				}
				counter += 1
				results[counter] = took
				// fmt.Println(counter, took)
				cpuSet = cpuSet.Difference(took)
			}
			result := fmt.Sprint(results)
			if result != tc.expResult {
				t.Errorf("[%s] expected %v to equal %v", tc.description, result, tc.expResult)
			}
		})
	}
}
