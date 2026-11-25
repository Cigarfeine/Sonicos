package command

import (
	"errors"
	"sync"
)

var (
	defaultRegistry = NewRegistry()
	registrationMu  sync.Mutex
	registrationErr error
)

func Register(cmd Command) {
	registrationMu.Lock()
	defer registrationMu.Unlock()
	if registrationErr != nil {
		return
	}
	registrationErr = defaultRegistry.Register(cmd)
}

func Watch(w Watcher) {
	defaultRegistry.Watch(w)
}

func Default() (*Registry, error) {
	registrationMu.Lock()
	defer registrationMu.Unlock()
	if registrationErr != nil {
		return nil, errors.New("command plugin registration failed: " + registrationErr.Error())
	}
	return defaultRegistry, nil
}
