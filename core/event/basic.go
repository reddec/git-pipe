package event

import "github.com/reddec/git-pipe/core"

func New(buffer int) *Emitter {
	return &Emitter{ready: make(chan struct{}, buffer)}
}

type Emitter struct {
	ready chan struct{}
}

func (em *Emitter) Ready() {
	select {
	case em.ready <- struct{}{}:
	default:
	}
}

func (em *Emitter) OnReady() <-chan struct{} {
	return em.ready
}

func Noop() core.Event {
	return &noop{}
}

type noop struct{}

func (n *noop) Ready() {}
