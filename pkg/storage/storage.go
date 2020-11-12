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
	"k8s.io/metrics/pkg/apis/metrics"

	"sigs.k8s.io/metrics-server/pkg/api"
)

// kubernetesCadvisorWindow is the max window used by cAdvisor for calculating
// CPU usage rate.  While it can vary, it's no more than this number, but may be
// as low as half this number (when working with no backoff).  It would be really
// nice if the kubelet told us this in the summary API...
var kubernetesCadvisorWindow = 30 * time.Second

// storage is a thread save storage for node and pod metrics
type storage struct {
	metrics sync.Map
}

var _ Storage = (*storage)(nil)

func NewStorage() *storage {
	return &storage{}
}

// TODO(directxman12): figure out what the right value is for "window" --
// we don't get the actual window from cAdvisor, so we could just
// plumb down metric resolution, but that wouldn't be actually correct.
func (s *storage) GetNodeMetrics(nodes ...*corev1.Node) ([]api.TimeInfo, []corev1.ResourceList) {
	timestamps := make([]api.TimeInfo, len(nodes))
	resMetrics := make([]corev1.ResourceList, len(nodes))

	for i, node := range nodes {
		value, ok := s.metrics.Load(node.Name)
		if !ok {
			continue
		}
		metricPoint := value.(*MetricsBatch).Node

		timestamps[i] = api.TimeInfo{
			Timestamp: metricPoint.Timestamp,
			Window:    kubernetesCadvisorWindow,
		}
		resMetrics[i] = corev1.ResourceList{
			corev1.ResourceName(corev1.ResourceCPU):    metricPoint.CpuUsage,
			corev1.ResourceName(corev1.ResourceMemory): metricPoint.MemoryUsage,
		}
	}

	return timestamps, resMetrics
}

func (s *storage) GetPodMetrics(pods ...*corev1.Pod) ([]api.TimeInfo, [][]metrics.ContainerMetrics) {
	timestamps := make([]api.TimeInfo, len(pods))
	resMetrics := make([][]metrics.ContainerMetrics, len(pods))


	for i, pod := range pods {
		value, ok := s.metrics.Load(pod.Spec.NodeName)
		if !ok {
			continue
		}
		points := value.(*MetricsBatch).Pods
		pos := sort.Search(len(points), func(i int) bool {
			return points[i].Namespace > pod.Namespace || (points[i].Namespace == pod.Namespace && points[i].Name >= pod.Name)
		})
		if pos >= len(points) {
			continue
		}
		metric := points[pos]
		if metric.Namespace != pod.Namespace || metric.Name != pod.Name {
			continue
		}

		contMetrics := make([]metrics.ContainerMetrics, len(metric.Containers))
		var earliestTS *time.Time
		for i, contPoint := range metric.Containers {
			contMetrics[i] = metrics.ContainerMetrics{
				Name: contPoint.Name,
				Usage: corev1.ResourceList{
					corev1.ResourceName(corev1.ResourceCPU):    contPoint.CpuUsage,
					corev1.ResourceName(corev1.ResourceMemory): contPoint.MemoryUsage,
				},
			}
			if earliestTS == nil || earliestTS.After(contPoint.Timestamp) {
				ts := contPoint.Timestamp // copy to avoid loop iteration variable issues
				earliestTS = &ts
			}
		}
		if earliestTS == nil {
			// we had no containers
			earliestTS = &time.Time{}
		}
		timestamps[i] = api.TimeInfo{
			Timestamp: *earliestTS,
			Window:    kubernetesCadvisorWindow,
		}
		resMetrics[i] = contMetrics
	}
	return timestamps, resMetrics
}

func (s *storage) Store(nodeName string, batch *MetricsBatch) {
	sort.Slice(batch.Pods, func(i, j int) bool {
		if batch.Pods[i].Namespace != batch.Pods[j].Namespace {
			return batch.Pods[i].Namespace < batch.Pods[j].Namespace
		}
		return batch.Pods[i].Name < batch.Pods[j].Name
	})
	s.metrics.Store(nodeName, batch)
	//pointsstored.withlabelvalues("node").set(float64(nodecount))
	//pointsstored.withlabelvalues("container").set(float64(containercount))
}

func (s *storage) Delete(nodeName string) {
	s.metrics.Delete(nodeName)
	//pointsstored.withlabelvalues("node").set(float64(nodecount))
	//pointsstored.withlabelvalues("container").set(float64(containercount))
}
