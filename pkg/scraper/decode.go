// Copyright 2020 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package scraper

import (
	"fmt"
	"math"
	"time"

	"k8s.io/klog/v2"

	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/metrics-server/pkg/storage"
)

func decodeBatch(summary *Summary) *storage.MetricsBatch {
	pods := make([]storage.PodMetricsPoint, 0, len(summary.Pods))
	for _, pod := range summary.Pods {
		pod := decodePodStats(&pod)
		if pod != nil {
			pods = append(pods, *pod)
		}
	}
	return &storage.MetricsBatch{
		Pods: pods,
		Node: decodeNodeStats(&summary.Node),
	}
}

func decodeNodeStats(nodeStats *NodeStats) *storage.NodeMetricsPoint {
	timestamp, err := getScrapeTime(nodeStats.CPU, nodeStats.Memory)
	if err != nil {
		// if we can't get a timestamp, assume bad data in general
		klog.V(1).Infof("Skip metric for node %q, error: %v", nodeStats.NodeName, err)
		return nil
	}
	target := storage.NodeMetricsPoint{
		Name: nodeStats.NodeName,
		MetricsPoint: storage.MetricsPoint{
			Timestamp: timestamp,
		},
	}
	if err := decodeCPU(&target.CpuUsage, nodeStats.CPU); err != nil {
		klog.V(1).Infof("Skip CPU metric for node %q, error %v", nodeStats.NodeName, err)
		return nil
	}
	if err := decodeMemory(&target.MemoryUsage, nodeStats.Memory); err != nil {
		klog.V(1).Infof("Skip Memory metric for node %q, error %v", nodeStats.NodeName, err)
		return nil
	}
	return &target
}

func decodePodStats(podStats *PodStats) *storage.PodMetricsPoint {
	target := storage.PodMetricsPoint{
		Name:       podStats.PodRef.Name,
		Namespace:  podStats.PodRef.Namespace,
		Containers: make([]storage.ContainerMetricsPoint, len(podStats.Containers)),
	}
	for i, container := range podStats.Containers {
		timestamp, err := getScrapeTime(container.CPU, container.Memory)
		if err != nil {
			// if we can't get a timestamp, assume bad data in general
			klog.V(1).Infof("Skip container %q in pod %s/%s, error: %v", container.Name, target.Namespace, target.Name, err)
			return nil
		}
		point := storage.ContainerMetricsPoint{
			Name: container.Name,
			MetricsPoint: storage.MetricsPoint{
				Timestamp: timestamp,
			},
		}
		if err = decodeCPU(&point.CpuUsage, container.CPU); err != nil {
			klog.V(1).Infof("Skip CPU metric for container %q in pod %s/%s, error: %v", container.Name, target.Namespace, target.Name, err)
			return nil
		}
		if err = decodeMemory(&point.MemoryUsage, container.Memory); err != nil {
			klog.V(1).Infof("Skip Memory metric for container %q in pod %s/%s, error: %v", container.Name, target.Namespace, target.Name, err)
			return nil
		}

		target.Containers[i] = point
	}
	return &target
}

func decodeCPU(target *resource.Quantity, cpuStats *CPUStats) error {
	if cpuStats == nil || cpuStats.UsageNanoCores == nil {
		return fmt.Errorf("missing usageNanoCores value")
	}

	*target = *uint64Quantity(*cpuStats.UsageNanoCores, -9)
	return nil
}

func decodeMemory(target *resource.Quantity, memStats *MemoryStats) error {
	if memStats == nil || memStats.WorkingSetBytes == nil {
		return fmt.Errorf("missing workingSetBytes value")
	}

	*target = *uint64Quantity(*memStats.WorkingSetBytes, 0)
	target.Format = resource.BinarySI

	return nil
}

func getScrapeTime(cpu *CPUStats, memory *MemoryStats) (time.Time, error) {
	// Ensure we get the earlier timestamp so that we can tell if a given data
	// point was tainted by pod initialization.

	var earliest *time.Time
	if cpu != nil && !cpu.Time.IsZero() && (earliest == nil || earliest.After(cpu.Time.Time)) {
		earliest = &cpu.Time.Time
	}

	if memory != nil && !memory.Time.IsZero() && (earliest == nil || earliest.After(memory.Time.Time)) {
		earliest = &memory.Time.Time
	}

	if earliest == nil {
		return time.Time{}, fmt.Errorf("no non-zero timestamp on either CPU or memory")
	}

	return *earliest, nil
}

// uint64Quantity converts a uint64 into a Quantity, which only has constructors
// that work with int64 (except for parse, which requires costly round-trips to string).
// We lose precision until we fit in an int64 if greater than the max int64 value.
func uint64Quantity(val uint64, scale resource.Scale) *resource.Quantity {
	// easy path -- we can safely fit val into an int64
	if val <= math.MaxInt64 {
		return resource.NewScaledQuantity(int64(val), scale)
	}

	klog.V(2).Infof("unexpectedly large resource value %v, loosing precision to fit in scaled resource.Quantity", val)

	// otherwise, lose an decimal order-of-magnitude precision,
	// so we can fit into a scaled quantity
	return resource.NewScaledQuantity(int64(val/10), resource.Scale(1)+scale)
}
