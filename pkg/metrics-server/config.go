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
package metric_server

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/component-base/metrics"
	"time"

	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"sigs.k8s.io/metrics-server/pkg/api"
	"sigs.k8s.io/metrics-server/pkg/scraper"
	"sigs.k8s.io/metrics-server/pkg/storage"
)

var (
	pointsAvailable metrics.GaugeFunc
)

type Config struct {
	Apiserver        *genericapiserver.Config
	Rest             *rest.Config
	Kubelet          *scraper.KubeletClientConfig
	MetricResolution time.Duration
	ScrapeTimeout    time.Duration
}

func (c Config) Complete() (*MetricsServer, error) {
	informer, err := c.informer()
	if err != nil {
		return nil, err
	}
	kubeletClient, err := c.Kubelet.Complete()
	if err != nil {
		return nil, fmt.Errorf("unable to construct a client to connect to the kubelets: %v", err)
	}
	corev1 := informer.Core().V1()
	nodes := corev1.Nodes()
	scrape := scraper.NewScraper(nodes.Lister(), kubeletClient, c.ScrapeTimeout)

	scraper.RegisterScraperMetrics(c.ScrapeTimeout)
	RegisterServerMetrics(c.MetricResolution)

	genericServer, err := c.Apiserver.Complete(informer).New("metrics-server", genericapiserver.NewEmptyDelegate())
	if err != nil {
		return nil, err
	}

	store := storage.NewStorage()
	if err := api.Install(store, corev1, genericServer); err != nil {
		return nil, err
	}


	pointsAvailable = metrics.NewGaugeFunc(
		metrics.GaugeOpts{
			Namespace: "metrics_server",
			Name:      "points_available",
			Help:      "Number of metrics points that is available in cluster.",
		},
		func() float64 {
			nodeList, err := corev1.Nodes().Lister().List(nil)
			if err != nil {
				return 0
			}
			nodeCount := 0
			for _, node := range nodeList {
				for _, condition := range node.Status.Conditions {
					if condition.Type == v1.NodeReady && condition.Status == v1.ConditionTrue {
						nodeCount += 1
					}
				}
			}
			podList, err := corev1.Pods().Lister().List(nil)
			if err != nil {
				return 0
			}
			containerCount := 0
			for _, pod := range podList {
				for _, status := range pod.Status.ContainerStatuses {
					if status.State.Running != nil {
						containerCount += 1
					}
				}
			}
			return float64(nodeCount + containerCount)
		},
	)
	return &MetricsServer{
		syncs:            []cache.InformerSynced{nodes.Informer().HasSynced},
		informer:         informer,
		GenericAPIServer: genericServer,
		storage:          store,
		scraper:          scrape,
		resolution:       c.MetricResolution,
	}, nil
}

func (c Config) informer() (informers.SharedInformerFactory, error) {
	// set up the informers
	kubeClient, err := kubernetes.NewForConfig(c.Rest)
	if err != nil {
		return nil, fmt.Errorf("unable to construct lister client: %v", err)
	}
	// we should never need to resync, since we're not worried about missing events,
	// and resync is actually for regular interval-based reconciliation these days,
	// so set the default resync interval to 0
	return informers.NewSharedInformerFactory(kubeClient, 0), nil
}
