package provider

import (
	"fmt"
	"strings"
)

// Registry manages the set of supported AI CLI providers.
type Registry struct {
	providers map[string]Provider
	order     []string
}

// NewRegistry creates a registry initialized with all standard providers.
func NewRegistry(providers ...Provider) *Registry {
	r := &Registry{
		providers: make(map[string]Provider),
		order:     make([]string, 0),
	}

	for _, p := range providers {
		r.Register(p)
	}

	return r
}

// DefaultRegistry creates a registry with all default providers.
func DefaultRegistry() *Registry {
	return NewRegistry(
		NewClaude(""),
		NewCodex(""),
		NewVibe(""),
		NewAgy(""),
	)
}

// Register registers a provider in the registry.
func (r *Registry) Register(p Provider) {
	name := strings.ToLower(p.Name())
	r.providers[name] = p
	r.order = append(r.order, name)
}

// Get looks up a provider by name.
func (r *Registry) Get(name string) (Provider, error) {
	p, ok := r.providers[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q (supported: %s)", name, strings.Join(r.order, ", "))
	}
	return p, nil
}

// All returns all registered providers in priority order.
func (r *Registry) All() []Provider {
	result := make([]Provider, 0, len(r.order))
	for _, name := range r.order {
		if p, ok := r.providers[name]; ok {
			result = append(result, p)
		}
	}
	return result
}

// NextAvailable returns the next provider in the queue after currentProvider.
func (r *Registry) NextAvailable(current string) Provider {
	curr := strings.ToLower(strings.TrimSpace(current))
	if len(r.order) == 0 {
		return nil
	}

	for i, name := range r.order {
		if name == curr {
			nextIdx := (i + 1) % len(r.order)
			return r.providers[r.order[nextIdx]]
		}
	}

	return r.providers[r.order[0]]
}
