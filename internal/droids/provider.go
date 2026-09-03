package droids

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// provider.go — the multiprovider registry. NewProviders composes one or more
// Provider configs into a single Providers registry that routes each request to
// the provider that owns the model. This mirrors pi-ai's Models registry.

// Providers is the model-abstraction layer: a registry over one or more
// Provider configs. It lists models and streams a request against whichever
// registered provider owns the target model.
type Providers interface {
	// Models returns every model across all registered providers.
	Models() []Model
	// Model resolves a user-facing id to a concrete model. The id may be bare
	// ("gpt-5.6") or namespaced ("openai/gpt-5.6") to disambiguate.
	Model(id string) (Model, bool)
	// RefreshModels fetches the latest models.dev catalog and overlays complete
	// model metadata onto each built-in provider. Existing models remain usable
	// when refresh fails.
	RefreshModels(ctx context.Context) error
	// Stream runs a request against the provider that owns model.
	Stream(ctx context.Context, model Model, req Request) Stream
}

// Provider is a single provider's configuration (e.g. OpenAI). Each one knows
// how to build itself into an internal entry (models + stream fn + auth), and
// is composed into a Providers registry by NewProviders.
type Provider interface {
	build() (providerEntry, error)
}

// providerEntry is the internal, resolved form of a Provider.
type providerEntry struct {
	id              string
	catalogID       string
	baseURL         string
	models          map[string]Model
	canonicalModels bool
	stream          streamFn
	call            callOptions
}

type registry struct {
	mu      sync.RWMutex
	entries map[string]providerEntry // by provider id
	// index maps a bare model id to its owning provider id. Ambiguous ids
	// (served by multiple providers) are omitted; callers must namespace.
	index     map[string]string
	ambiguous map[string]bool
}

// NewProviders composes provider configs into a single routing Providers registry.
func NewProviders(configs ...Provider) (Providers, error) {
	if len(configs) == 0 {
		return nil, fmt.Errorf("droids: NewProviders requires at least one Provider")
	}
	r := &registry{
		entries:   map[string]providerEntry{},
		index:     map[string]string{},
		ambiguous: map[string]bool{},
	}
	for _, cfg := range configs {
		entry, err := cfg.build()
		if err != nil {
			return nil, err
		}
		if _, dup := r.entries[entry.id]; dup {
			return nil, fmt.Errorf("droids: duplicate provider id %q", entry.id)
		}
		r.entries[entry.id] = entry
	}
	r.rebuildIndex()
	return r, nil
}

func (r *registry) Models() []Model {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Model
	for _, e := range r.entries {
		for _, m := range e.models {
			out = append(out, cloneModel(m))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider == out[j].Provider {
			return out[i].ID < out[j].ID
		}
		return out[i].Provider < out[j].Provider
	})
	return out
}

func (r *registry) Model(id string) (Model, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	// Namespaced form: "provider/model".
	if provID, modelID, ok := strings.Cut(id, "/"); ok {
		if e, exists := r.entries[provID]; exists {
			if m, exists := e.models[modelID]; exists {
				return cloneModel(m), true
			}
		}
		// fall through: maybe the id legitimately contains a slash
	}
	if r.ambiguous[id] {
		return Model{}, false // caller must namespace
	}
	if provID, ok := r.index[id]; ok {
		m := r.entries[provID].models[id]
		return cloneModel(m), true
	}
	return Model{}, false
}

func (r *registry) RefreshModels(ctx context.Context) error {
	return r.refreshModels(ctx, modelsDevCatalogURL)
}

func (r *registry) refreshModels(ctx context.Context, catalogURL string) error {
	type target struct {
		providerID string
		catalogID  string
		baseURL    string
	}
	r.mu.RLock()
	targets := make([]target, 0, len(r.entries))
	for _, entry := range r.entries {
		if entry.catalogID != "" {
			targets = append(targets, target{
				providerID: entry.id,
				catalogID:  entry.catalogID,
				baseURL:    entry.baseURL,
			})
		}
	}
	r.mu.RUnlock()
	if len(targets) == 0 {
		return nil
	}

	catalog, err := fetchModelCatalog(ctx, catalogURL)
	if err != nil {
		return err
	}

	// Translation and sorting can be substantial for a remote catalog. Build
	// overlays without blocking model lookup or streaming.
	updates := make(map[string][]Model, len(targets))
	for _, target := range targets {
		models := catalogModels(catalog, target.catalogID, target.providerID)
		setModelBaseURL(models, target.baseURL)
		updates[target.providerID] = models
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for providerID, models := range updates {
		entry, ok := r.entries[providerID]
		if !ok || len(models) == 0 {
			continue
		}
		for _, model := range models {
			entry.models[model.ID] = model
		}
		r.entries[providerID] = entry
	}
	r.rebuildIndex()
	return nil
}

func (r *registry) rebuildIndex() {
	r.index = map[string]string{}
	r.ambiguous = map[string]bool{}
	for providerID, entry := range r.entries {
		for modelID := range entry.models {
			if previous, exists := r.index[modelID]; exists && previous != providerID {
				delete(r.index, modelID)
				r.ambiguous[modelID] = true
				continue
			}
			if !r.ambiguous[modelID] {
				r.index[modelID] = providerID
			}
		}
	}
}

func (r *registry) Stream(ctx context.Context, model Model, req Request) Stream {
	requested := model
	r.mu.RLock()
	e, providerExists := r.entries[requested.Provider]
	canonical, modelExists := e.models[requested.ID]
	r.mu.RUnlock()
	if !providerExists {
		return erroredStream(requested, fmt.Sprintf("unknown provider %q", requested.Provider))
	}
	if !modelExists {
		return erroredStream(requested, fmt.Sprintf("provider %q does not own model %q", requested.Provider, requested.ID))
	}
	// Providers with immutable, non-refreshable catalogs may opt into exact
	// canonical metadata. Refreshable providers retain the caller's registry
	// snapshot so existing Droids do not mix old and newly refreshed limits.
	if e.canonicalModels {
		requested = canonical
	}
	if _, err := resolveRequestMaxTokens(requested, req.MaxTokens, req.Reasoning); err != nil {
		return erroredStream(requested, err.Error())
	}
	return e.stream(ctx, requested, req, e.call)
}

// erroredStream returns a Stream that immediately fails. Used for
// configuration errors so failures flow through the normal stream protocol.
func erroredStream(model Model, msg string) Stream {
	final := AssistantMessage{
		Provider:     model.Provider,
		Model:        model.ID,
		StopReason:   StopReasonError,
		ErrorMessage: msg,
		Timestamp:    time.Now().UnixMilli(),
	}
	ch := make(chan StreamEvent, 1)
	ch <- StreamError{Message: final}
	close(ch)
	return &staticStream{events: ch, final: final}
}

// staticStream is a trivial Stream backed by a prebuilt channel + final message.
type staticStream struct {
	events <-chan StreamEvent
	final  AssistantMessage
}

func (s *staticStream) Events() <-chan StreamEvent { return s.events }
func (s *staticStream) Result() AssistantMessage   { return s.final }
