package stoppablemanager

import (
	"context"
	"errors"

	"k8s.io/client-go/rest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var log = logf.Log.WithName("stoppable_manager")

//StoppableManager A StoppableManaager allows you to easily create controller-runtim.Managers that can be started and stopped.
type StoppableManager struct {
	started bool
	manager.Manager
	cancelFunction context.CancelFunc
}

//Stop stops the manager
func (sm *StoppableManager) Stop() {
	if !sm.started {
		log.Error(errors.New("invalid argument"), "stop called on a non started channel", "started", sm.started)
		return
	}
	sm.cancelFunction()
	//close(sm.stopChannel)
	sm.started = false
}

//Start starts the manager. Restarting a starated manager is a noop that will be logged.
func (sm *StoppableManager) Start(parentCtx context.Context) {
	if sm.started {
		log.Error(errors.New("invalid argument"), "start called on a started channel")
		return
	}
	ctx, cancel := context.WithCancel(parentCtx)
	sm.cancelFunction = cancel
	go func() {
		err := sm.Manager.Start(ctx)
		if err != nil {
			log.Error(errors.New("unable to start manager"), "unable to start manager")
		}
	}()
	sm.started = true
}

//NewStoppableManager creates a new stoppable manager
func NewStoppableManager(config *rest.Config, options manager.Options) (StoppableManager, error) {
	manager, err := manager.New(config, options)
	if err != nil {
		return StoppableManager{}, err
	}
	return StoppableManager{
		Manager: manager,
	}, nil
}

//IsStarted returns wether this stoppable manager is running.
func (sm *StoppableManager) IsStarted() bool {
	return sm.started
}
