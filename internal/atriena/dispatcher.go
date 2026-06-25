package atriena

import (
	"context"
	"fmt"
	"sync"
)

type Dispatcher interface {
	DispatchAtriena(ctx context.Context, origin Origin, req Request) (Response, error)
}

type Bus struct {
	mu         sync.RWMutex
	dispatcher Dispatcher
}

func NewBus() *Bus {
	return &Bus{}
}

func (b *Bus) SetDispatcher(dispatcher Dispatcher) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.dispatcher = dispatcher
}

func (b *Bus) DispatchAtriena(ctx context.Context, origin Origin, req Request) (Response, error) {
	b.mu.RLock()
	dispatcher := b.dispatcher
	b.mu.RUnlock()
	if dispatcher == nil {
		return Response{Accepted: false, Status: StatusFailed, Error: "atriena dispatcher is not configured"}, fmt.Errorf("atriena dispatcher is not configured")
	}
	return dispatcher.DispatchAtriena(ctx, origin, req)
}
