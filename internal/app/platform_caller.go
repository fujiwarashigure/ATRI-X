package app

import (
	"strings"

	"github.com/fujiwarashigure/ATRI-X/internal/atriena"
)

type platformCallerResolver struct {
	runtimes []platformRuntime
}

func (r platformCallerResolver) PlatformCaller(name string) (atriena.PlatformAPICaller, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, false
	}
	for _, runtime := range r.runtimes {
		if runtime == nil || runtime.Name() != name {
			continue
		}
		caller, ok := runtime.(atriena.PlatformAPICaller)
		return caller, ok
	}
	return nil, false
}
