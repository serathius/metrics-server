module sigs.k8s.io/metrics-server

go 1.14

require (
	github.com/NYTimes/gziphandler v1.1.1 // indirect
	github.com/emicklei/go-restful v2.12.0+incompatible // indirect
	github.com/go-openapi/spec v0.19.7
	github.com/go-openapi/swag v0.19.9 // indirect
	github.com/golang/groupcache v0.0.0-20200121045136-8c9f03a8e57e // indirect
	github.com/golang/protobuf v1.4.0 // indirect
	github.com/hashicorp/golang-lru v0.5.4 // indirect
	github.com/imdario/mergo v0.3.9 // indirect
	github.com/mailru/easyjson v0.7.1 // indirect
	github.com/onsi/ginkgo v1.11.0
	github.com/onsi/gomega v1.9.0
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/client_golang v1.5.1 // indirect
	github.com/prometheus/common v0.9.1
	github.com/prometheus/procfs v0.0.11 // indirect
	github.com/spf13/cobra v1.0.0
	go.uber.org/zap v1.14.1 // indirect
	golang.org/x/crypto v0.0.0-20200420201142-3c4aac89819a // indirect
	golang.org/x/oauth2 v0.0.0-20200107190931-bf48bf16ab8d // indirect
	golang.org/x/sync v0.0.0-20200317015054-43a5402ce75a // indirect
	golang.org/x/sys v0.0.0-20200420163511-1957bb5e6d1f // indirect
	golang.org/x/time v0.0.0-20200416051211-89c76fbcd5d1 // indirect
	google.golang.org/appengine v1.6.5 // indirect
	k8s.io/api v0.18.2
	k8s.io/apimachinery v0.18.2
	k8s.io/apiserver v0.18.2
	k8s.io/client-go v0.18.2
	k8s.io/component-base v0.18.2
	k8s.io/klog v1.0.0
	k8s.io/kube-openapi v0.0.0-20200121204235-bf4fb3bd569c
	k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1 v0.0.0
	k8s.io/metrics v0.18.2
	k8s.io/utils v0.0.0-20200414100711-2df71ebbae66 // indirect
	sigs.k8s.io/apiserver-network-proxy/konnectivity-client v0.0.8 // indirect
)

replace k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1 => ./vendor/k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1
