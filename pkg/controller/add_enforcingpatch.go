package controller

import (
	"github.com/redhat-cop/operator-utils/pkg/controller/enforcingpatch"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, enforcingpatch.Add)
}
