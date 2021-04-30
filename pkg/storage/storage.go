// Copyright 2018 The Kubernetes Authors.
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

package storage

import (
	"sort"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/metrics/pkg/apis/metrics"

	"sigs.k8s.io/metrics-server/pkg/api"
)

// storage is a thread save storage for node and pod metrics.
//
// This implementation only stores metric points if they are newer than the
// points already stored and the cpuUsageOverTime function used to handle
// cumulative metrics assumes that the time window is different from 0.
type storage struct {
	mu    sync.RWMutex
	nodes map[string]storedMetric
	pods  map[apitypes.NamespacedName][]storedContainerMetric
}

var _ Storage = (*storage)(nil)

func NewStorage() *storage {
	return &storage{
		nodes: map[string]storedMetric{},
		pods:  map[apitypes.NamespacedName][]storedContainerMetric{},
	}
}

// Ready returns true if metrics-server's storage has accumulated enough metric
// points to serve NodeMetrics.
// Checking only nodes for metrics-server's storage readiness is enough to make
// sure that it has accumulated enough metrics to serve both NodeMetrics and
// PodMetrics. It also covers cases where metrics-server only has to serve
// NodeMetrics.
func (s *storage) Ready() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, m := range s.nodes {
		if !m.ready() {
			return true
		}
	}
	return false
}

func (s *storage) GetNodeMetrics(nodes ...string) ([]api.TimeInfo, []corev1.ResourceList, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ts := make([]api.TimeInfo, len(nodes))
	ms := make([]corev1.ResourceList, len(nodes))
	for i, node := range nodes {
		m, found := s.nodes[node]
		if !found || !m.ready() {
			continue
		}
		ms[i], ts[i] = m.usage()
	}
	return ts, ms, nil
}

func (s *storage) GetContainerMetrics(pods ...apitypes.NamespacedName) ([]api.TimeInfo, [][]metrics.ContainerMetrics, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	timestamps := make([]api.TimeInfo, len(pods))
	resMetrics := make([][]metrics.ContainerMetrics, len(pods))
	for i, pod := range pods {
		ms, found := s.pods[pod]
		if !found {
			continue
		}
		var (
			cms        = make([]metrics.ContainerMetrics, 0, len(ms))
			earliestTs api.TimeInfo
		)
		for _, m := range ms {
			if !m.ready() {
				continue
			}
			cm, ts := m.usage()
			cms = append(cms, metrics.ContainerMetrics{Name: m.name, Usage: cm})
			if earliestTs.Timestamp.IsZero() || earliestTs.Timestamp.After(ts.Timestamp) {
				earliestTs = ts
			}
		}

		if len(cms) != 0 {
			resMetrics[i] = cms
			timestamps[i] = earliestTs
		}
	}
	return timestamps, resMetrics, nil
}

func (s *storage) Store(batch *MetricsBatch) {
	s.storeNodeMetrics(batch.Nodes)
	s.storePodMetrics(batch.Pods)
}

func (s *storage) storeNodeMetrics(nodes []NodeMetricsPoint) {
	s.mu.Lock()
	defer s.mu.Unlock()

	toDelete := make(map[string]struct{}, len(s.nodes))
	for node := range s.nodes {
		toDelete[node] = struct{}{}
	}
	var nodeCount float64
	for _, nodePoint := range nodes {
		delete(toDelete, nodePoint.Name)
		node := s.nodes[nodePoint.Name]
		node.merge(nodePoint.MetricsPoint)
		s.nodes[nodePoint.Name] = node
		if node.ready() {
			nodeCount += 1
		}
	}
	for node := range toDelete {
		delete(s.nodes, node)
	}

	// Only count nodes for which metrics can be returned.
	pointsStored.WithLabelValues("node").Set(nodeCount)
}

func (s *storage) storePodMetrics(pods []PodMetricsPoint) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var containerCount float64
	toDelete := make(map[apitypes.NamespacedName]struct{}, len(s.pods))
	for pod := range s.pods {
		toDelete[pod] = struct{}{}
	}
	for _, pod := range pods {
		key := apitypes.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}
		delete(toDelete, key)
		sort.Slice(pod.Containers, func(i, j int) bool {
			return pod.Containers[i].Name < pod.Containers[j].Name
		})
		sc, found := s.pods[key]
		match := len(sc) == len(pod.Containers)
		if match {
			for i := range sc {
				if sc[i].name != pod.Containers[i].Name {
					match = false
					break
				}
			}
		}
		if !found {
			// New Pod, allocate new list
			cs := make([]storedContainerMetric, len(pod.Containers))
			for i := range pod.Containers {
				cs[i].name = pod.Containers[i].Name
				cs[i].merge(pod.Containers[i].MetricsPoint)
			}
			s.pods[key] = cs
		} else if match {
			// Containers match, just merge
			for i := range pod.Containers {
				sc[i].merge(pod.Containers[i].MetricsPoint)
				if sc[i].ready() {
					containerCount++
				}
			}
		} else {
			// Containers differ, allocate new list and merge
			cs := make([]storedContainerMetric, len(pod.Containers))
			var i, j int
			for i < len(pod.Containers) && j < len(sc) {
				if pod.Containers[i].Name == sc[j].name {
					cs[i].name = pod.Containers[i].Name
					cs[i].storedMetric = *sc[j].merge(pod.Containers[i].MetricsPoint)
					if cs[i].ready() {
						containerCount++
					}
					i++
					j++
				} else if pod.Containers[i].Name < sc[j].name {
					cs[i].name = pod.Containers[i].Name
					cs[i].merge(pod.Containers[i].MetricsPoint)
					i++
				} else {
					j++
				}
			}
			for i < len(pod.Containers) {
				cs[i].name = pod.Containers[i].Name
				cs[i].merge(pod.Containers[i].MetricsPoint)
				i++
			}
			s.pods[key] = cs
		}

	}
	for pod := range toDelete {
		delete(s.pods, pod)
	}

	pointsStored.WithLabelValues("container").Set(containerCount)
}

type storedMetric struct {
	index       int
	timestamp   [2]time.Time
	memoryUsage resource.Quantity
	cpuUsage    [2]resource.Quantity
}

type storedContainerMetric struct {
	name string
	storedMetric
}

func (m storedMetric) usage() (corev1.ResourceList, api.TimeInfo) {
	current := m.index
	prev := (m.index + 1) % 2

	window := m.timestamp[current].Sub(m.timestamp[prev])
	cpuUsageScaled := m.cpuUsage[current].ScaledValue(-9)
	prevCPUUsageScaled := m.cpuUsage[prev].ScaledValue(-9)
	cpuUsage := float64(cpuUsageScaled-prevCPUUsageScaled) / window.Seconds()
	return corev1.ResourceList{
			corev1.ResourceCPU:    *resource.NewScaledQuantity(int64(cpuUsage), -9),
			corev1.ResourceMemory: m.memoryUsage,
		}, api.TimeInfo{
			Timestamp: m.timestamp[current],
			Window:    window,
		}
}

func (m *storedMetric) merge(p MetricsPoint) *storedMetric {
	if m.timestamp[m.index].IsZero() || p.Timestamp.After(m.timestamp[m.index]) {
		m.index = (m.index + 1) % 2
		m.timestamp[m.index] = p.Timestamp
		m.cpuUsage[m.index] = p.CpuUsage
		m.memoryUsage = p.MemoryUsage
	}
	return m
}

func (m storedMetric) ready() bool {
	return !m.timestamp[0].IsZero() && !m.timestamp[1].IsZero()
}
