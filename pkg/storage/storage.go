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
	"k8s.io/klog/v2"
	"k8s.io/metrics/pkg/apis/metrics"

	"sigs.k8s.io/metrics-server/pkg/api"
)

// storage is a thread save storage for node and pod metrics.
//
// This implementation only stores metric points if they are newer than the
// points already stored and the cpuUsageOverTime function used to handle
// cumulative metrics assumes that the time window is different from 0.
type storage struct {
	mu        sync.RWMutex
	nodes     map[string]storedMetric
	pods      map[apitypes.NamespacedName][]ContainerMetricsPoint
	prevPods  map[apitypes.NamespacedName][]ContainerMetricsPoint
}

var _ Storage = (*storage)(nil)

func NewStorage() *storage {
	return &storage{
		nodes: map[string]storedMetric{},
	}
}

// Ready returns true if metrics-server's storage has accumulated enough metric
// points to serve NodeMetrics.
// Checking only nodes for metrics-server's storage readiness is enough to make
// sure that it has accumulated enough metrics to serve both NodeMetrics and
// PodMetrics. It also covers cases where metrics-server only has to serve
// NodeMetrics.
func (p *storage) Ready() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, m := range p.nodes {
		if !m.ready() {
			return true
		}
	}
	return false
}

func (p *storage) GetNodeMetrics(nodes ...string) ([]api.TimeInfo, []corev1.ResourceList, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	ts := make([]api.TimeInfo, len(nodes))
	ms := make([]corev1.ResourceList, len(nodes))
	for i, node := range nodes {
		m, found := p.nodes[node]
		if !found || !m.ready() {
			continue
		}
		ms[i], ts[i] = m.usage()
	}
	return ts, ms, nil
}

func (p *storage) GetContainerMetrics(pods ...apitypes.NamespacedName) ([]api.TimeInfo, [][]metrics.ContainerMetrics, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	timestamps := make([]api.TimeInfo, len(pods))
	resMetrics := make([][]metrics.ContainerMetrics, len(pods))
	for podIdx, pod := range pods {
		contPoints, found := p.pods[pod]
		if !found {
			continue
		}

		prevPod, found := p.prevPods[pod]
		if !found {
			continue
		}

		var (
			contMetrics = make([]metrics.ContainerMetrics, 0, len(contPoints))
			earliestTS  time.Time
			window      time.Duration
		)
		var i, j int
		for i < len(contPoints) && j < len(prevPod) {
			if contPoints[i].Name == prevPod[j].Name {
				cpuUsage := cpuUsageOverTime(contPoints[i].MetricsPoint, prevPod[j].MetricsPoint)
				contMetrics = append(contMetrics, metrics.ContainerMetrics{
					Name: contPoints[i].Name,
					Usage: corev1.ResourceList{
						corev1.ResourceName(corev1.ResourceCPU):    cpuUsage,
						corev1.ResourceName(corev1.ResourceMemory): contPoints[i].MemoryUsage,
					},
				})
				if earliestTS.IsZero() || earliestTS.After(contPoints[i].Timestamp) {
					earliestTS = contPoints[i].Timestamp
					window = contPoints[i].Timestamp.Sub(prevPod[j].Timestamp)
				}
				i++
				j++
			} else if contPoints[i].Name < prevPod[j].Name {
				i++
			} else {
				j++
			}
		}
		resMetrics[podIdx] = contMetrics
		timestamps[podIdx] = api.TimeInfo{
			Timestamp: earliestTS,
			Window:    window,
		}
	}
	return timestamps, resMetrics, nil
}

func (p *storage) Store(batch *MetricsBatch) {
	p.storeNodeMetrics(batch.Nodes)
	p.storePodMetrics(batch.Pods)
}

func (p *storage) storeNodeMetrics(nodes []NodeMetricsPoint) {
	p.mu.Lock()
	defer p.mu.Unlock()

	toDelete := make(map[string]struct{}, len(p.nodes))
	for node := range p.nodes {
		toDelete[node] = struct{}{}
	}
	var nodeCount float64
	for _, nodePoint := range nodes {
		delete(toDelete, nodePoint.Name)
		node := p.nodes[nodePoint.Name]
		node.merge(nodePoint.MetricsPoint)
		p.nodes[nodePoint.Name] = node
		if node.ready() {
			nodeCount+=1
		}
	}
	for node := range toDelete {
		delete(p.nodes, node)
	}

	// Only count nodes for which metrics can be returned.
	pointsStored.WithLabelValues("node").Set(nodeCount)
}

func (p *storage) storePodMetrics(pods []PodMetricsPoint) {
	p.mu.Lock()
	defer p.mu.Unlock()

	newPods := make(map[apitypes.NamespacedName][]ContainerMetricsPoint, len(pods))
	prevPods := make(map[apitypes.NamespacedName][]ContainerMetricsPoint, len(pods))
	var containerCount int
	for _, podPoint := range pods {
		podIdent := apitypes.NamespacedName{Name: podPoint.Name, Namespace: podPoint.Namespace}
		if _, exists := newPods[podIdent]; exists {
			klog.ErrorS(nil, "Got duplicate pod point", "pod", klog.KRef(podPoint.Namespace, podPoint.Name))
			continue
		}

		newContainers := podPoint.Containers
		prevContainers := make([]ContainerMetricsPoint, 0, len(podPoint.Containers))

		sort.Slice(podPoint.Containers, func(i, j int) bool {
			return podPoint.Containers[i].Name < podPoint.Containers[j].Name
		})
		var i, j int
		storedContainers := p.pods[podIdent]

		for ; i < len(podPoint.Containers) && j < len(storedContainers); {
			// If the new point is newer than the one stored for the container, move
			// it to the list of the previous points.
			// This check also prevents from updating the store if the same metric
			// point was scraped twice.
			if podPoint.Containers[i].Name == storedContainers[j].Name {
				if podPoint.Containers[i].Timestamp.After(storedContainers[j].Timestamp) {
					prevContainers = append(prevContainers, storedContainers[i])
				} else {
					prevStoredContainers, found := p.prevPods[podIdent]
					if found {
						for k := 0; k < len(prevStoredContainers); k++ {
							if podPoint.Containers[i].Name == prevStoredContainers[k].Name {
								prevContainers = append(prevContainers, prevStoredContainers[k])
								break
							}
						}
					}
				}
				i++
				j++
			} else if podPoint.Containers[i].Name < storedContainers[j].Name {
				i++
			} else {
				j++
			}
		}
		containerPoints := len(prevContainers)
		if containerPoints > 0 {
			prevPods[podIdent] = prevContainers
		}
		newPods[podIdent] = newContainers

		// Only count containers for which metrics can be returned.
		containerCount += containerPoints
	}
	p.pods = newPods
	p.prevPods = prevPods

	pointsStored.WithLabelValues("container").Set(float64(containerCount))
}

func cpuUsageOverTime(metricPoint, prevMetricPoint MetricsPoint) resource.Quantity {
	window := metricPoint.Timestamp.Sub(prevMetricPoint.Timestamp).Seconds()
	cpuUsageScaled := metricPoint.CpuUsage.ScaledValue(-9)
	prevCPUUsageScaled := prevMetricPoint.CpuUsage.ScaledValue(-9)
	cpuUsage := float64(cpuUsageScaled-prevCPUUsageScaled) / window
	return *resource.NewScaledQuantity(int64(cpuUsage), -9)
}

type storedMetric struct {
	index       int
	timestamp   [2]time.Time
	memoryUsage resource.Quantity
	cpuUsage    [2]resource.Quantity
}

func (m storedMetric) usage() (corev1.ResourceList, api.TimeInfo)  {
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

func (m *storedMetric) merge(p MetricsPoint) {
	if m.timestamp[m.index].IsZero() || p.Timestamp.After(m.timestamp[m.index]) {
		m.index = (m.index + 1) % 2
		m.timestamp[m.index] = p.Timestamp
		m.cpuUsage[m.index] = p.CpuUsage
		m.memoryUsage = p.MemoryUsage
	}
}

func (m storedMetric) ready() bool {
	return !m.timestamp[0].IsZero() && !m.timestamp[1].IsZero()
}
