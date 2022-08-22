module github.com/redhat-cop/operator-utils

go 1.17

require (
	github.com/BurntSushi/toml v0.4.1
	github.com/Masterminds/sprig/v3 v3.2.2
	github.com/evanphx/json-patch v5.6.0+incompatible
	github.com/go-logr/logr v1.2.0
	github.com/hashicorp/go-multierror v1.1.1
	github.com/nsf/jsondiff v0.0.0-20210926074059-1e845ec5d249
	github.com/onsi/ginkgo v1.16.5
	github.com/onsi/gomega v1.17.0
	github.com/pkg/errors v0.9.1
	github.com/scylladb/go-set v1.0.2
	k8s.io/api v0.23.0
	k8s.io/apimachinery v0.23.0
	k8s.io/client-go v0.23.0
	k8s.io/kubectl v0.23.0
	sigs.k8s.io/controller-runtime v0.11.0
	sigs.k8s.io/yaml v1.3.0
)
