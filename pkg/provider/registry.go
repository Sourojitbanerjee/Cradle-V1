package provider

import (
	"fmt"
	"sync"

	"github.com/cradle-dev/cradle/pkg/domain"
)

// Registry manages registered cloud provider implementations.
type Registry struct {
	mu        sync.RWMutex
	providers map[domain.ProviderType]Provider
}

// Global default provider registry.
var DefaultRegistry = NewRegistry()

func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[domain.ProviderType]Provider),
	}
}

// Register adds a provider to the registry.
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
}

// Get retrieves a provider by type.
func (r *Registry) Get(pt domain.ProviderType) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[pt]
	if !ok {
		return nil, fmt.Errorf("provider '%s' is not registered", pt)
	}
	return p, nil
}
