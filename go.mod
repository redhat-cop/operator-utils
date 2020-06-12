module github.com/redhat-cop/operator-utils

require (
	github.com/evanphx/json-patch v4.5.0+incompatible
	github.com/fatih/set v0.2.1
	github.com/hashicorp/go-multierror v1.0.0
	github.com/operator-framework/operator-sdk v0.18.1
	github.com/prometheus/common v0.9.1
	github.com/scylladb/go-set v1.0.2
	github.com/spf13/pflag v1.0.5
	k8s.io/api v0.18.2
	k8s.io/apimachinery v0.18.2
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/kubernetes v1.13.0
	sigs.k8s.io/controller-runtime v0.6.0
	sigs.k8s.io/yaml v1.2.0
)

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v13.3.2+incompatible // Required by OLM
	k8s.io/client-go => k8s.io/client-go v0.18.2 // Required by prometheus-operator
)

go 1.13
