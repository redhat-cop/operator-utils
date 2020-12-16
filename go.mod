module github.com/redhat-cop/operator-utils

go 1.13

require (
	github.com/BurntSushi/toml v0.3.1
	github.com/Masterminds/sprig/v3 v3.1.0
	github.com/evanphx/json-patch v4.9.0+incompatible
	github.com/fatih/set v0.2.1
	github.com/go-logr/logr v0.3.0
	github.com/hashicorp/go-multierror v1.1.0
	github.com/onsi/ginkgo v1.14.1
	github.com/onsi/gomega v1.10.2
	github.com/pkg/errors v0.9.1
	github.com/prometheus/common v0.10.0
	github.com/scylladb/go-set v1.0.2
	k8s.io/api v0.20.0
	k8s.io/apimachinery v0.20.0
	k8s.io/client-go v0.20.0
	k8s.io/kubectl v0.20.0
	sigs.k8s.io/controller-runtime v0.7.0
	sigs.k8s.io/yaml v1.2.0
)
