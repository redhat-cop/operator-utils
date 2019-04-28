# Operator utility functions

Change your reconciler initialation as exeplified below to add a set of utility methods to it

```go
import "github.com/redhat-cop/operator-utils/pkg/util"

...

type MyReconciler struct {
  util.ReconcilerBase
  ... other optional fields ...
}

...

func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &MyReconciler{
		ReconcilerBase: util.NewReconcilerBase(mgr.GetClient(), mgr.GetScheme()),
	}
}
```