package stoppablemanager

import (
	"errors"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("stoppable_manager")

type StoppableManager struct {
	started bool
	manager.Manager
	stopChannel chan struct{}
}

func (sm *StoppableManager) Stop() {
	if !sm.started {
		log.Error(errors.New("invalid argument"), "stop called on a non started channel", "started", sm.started)
		return
	}
	close(sm.stopChannel)
	sm.started = false
}

func (sm *StoppableManager) Start() {
	if sm.started {
		log.Error(errors.New("invalid argument"), "start called on a started channel")
		return
	}
	go sm.Manager.Start(sm.stopChannel)
	sm.started = true
}

func NewStoppableManager(config *rest.Config, options manager.Options) (StoppableManager, error) {
	manager, err := manager.New(config, options)
	if err != nil {
		return StoppableManager{}, err
	}
	return StoppableManager{
		Manager:     manager,
		stopChannel: make(chan struct{}),
	}, nil
}

func (sm *StoppableManager) IsStarted() bool {
	return sm.started
}
