package sigil

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Config struct {
	OTLPHTTPEndpoint string
	RecordsEndpoint  string
	PayloadMaxBytes  int
	RecordStore      RecordStore
	Now              func() time.Time
}

func DefaultConfig() Config {
	return Config{
		OTLPHTTPEndpoint: "http://localhost:4318/v1/traces",
		RecordsEndpoint:  "http://localhost:8080/api/v1/records",
		PayloadMaxBytes:  8192,
		RecordStore:      NewMemoryRecordStore(),
		Now:              time.Now,
	}
}

type Client struct {
	config Config
}

func NewClient(config Config) *Client {
	cfg := config
	defaults := DefaultConfig()

	if cfg.OTLPHTTPEndpoint == "" {
		cfg.OTLPHTTPEndpoint = defaults.OTLPHTTPEndpoint
	}
	if cfg.RecordsEndpoint == "" {
		cfg.RecordsEndpoint = defaults.RecordsEndpoint
	}
	if cfg.RecordStore == nil {
		cfg.RecordStore = defaults.RecordStore
	}
	if cfg.Now == nil {
		cfg.Now = defaults.Now
	}

	return &Client{
		config: cfg,
	}
}

type GenerationHandle struct {
	client     *Client
	mu         sync.Mutex
	generation Generation
	finished   bool
}

func (c *Client) RecordGeneration(ctx context.Context, generation Generation) (GenerationRef, error) {
	g := cloneGeneration(generation)

	if g.ID == "" {
		g.ID = newRandomID("gen")
	}
	if g.StartedAt.IsZero() {
		g.StartedAt = c.now().UTC()
	}
	if g.CompletedAt.IsZero() {
		g.CompletedAt = c.now().UTC()
	}

	g.Usage = g.Usage.Normalize()

	if err := ValidateGeneration(g); err != nil {
		return GenerationRef{}, err
	}

	artifactRefs, err := c.externalizeArtifacts(ctx, g.Artifacts)
	if err != nil {
		return GenerationRef{}, err
	}

	return GenerationRef{
		GenerationID: g.ID,
		ArtifactRefs: artifactRefs,
	}, nil
}

func (c *Client) StartGeneration(ctx context.Context, start GenerationStart) (*GenerationHandle, context.Context, error) {
	g := start.toGeneration()
	if g.ID == "" {
		g.ID = newRandomID("gen")
	}
	if g.StartedAt.IsZero() {
		g.StartedAt = c.now().UTC()
	}

	if err := ValidateGeneration(g); err != nil {
		return nil, nil, err
	}

	handle := &GenerationHandle{
		client:     c,
		generation: g,
	}

	return handle, ctx, nil
}

func (h *GenerationHandle) SetGeneration(g Generation) {
	h.mu.Lock()
	defer h.mu.Unlock()

	next := cloneGeneration(g)

	if next.ID == "" {
		next.ID = h.generation.ID
	}
	if next.StartedAt.IsZero() {
		next.StartedAt = h.generation.StartedAt
	}
	if next.Model == (ModelRef{}) {
		next.Model = h.generation.Model
	}
	if next.ThreadID == "" {
		next.ThreadID = h.generation.ThreadID
	}

	h.generation = next
}

func (h *GenerationHandle) Finish(ctx context.Context, callErr error) (GenerationRef, error) {
	h.mu.Lock()
	if h.finished {
		h.mu.Unlock()
		return GenerationRef{}, errors.New("generation already finished")
	}

	g := cloneGeneration(h.generation)
	h.finished = true
	h.mu.Unlock()

	if callErr != nil {
		g.CallError = callErr.Error()
		if g.Metadata == nil {
			g.Metadata = map[string]any{}
		}
		g.Metadata["call_error"] = callErr.Error()
	}
	if g.CompletedAt.IsZero() {
		g.CompletedAt = h.client.now().UTC()
	}

	return h.client.RecordGeneration(ctx, g)
}

func (c *Client) now() time.Time {
	return c.config.Now()
}

func (c *Client) externalizeArtifacts(ctx context.Context, artifacts []Artifact) ([]ArtifactRef, error) {
	if len(artifacts) == 0 {
		return nil, nil
	}

	if c.config.RecordStore == nil {
		return nil, nil
	}

	refs := make([]ArtifactRef, 0, len(artifacts))
	for i := range artifacts {
		if len(artifacts[i].Payload) == 0 && artifacts[i].RecordID == "" {
			continue
		}

		recordID := artifacts[i].RecordID
		if recordID == "" {
			recordID = newRandomID("rec")

			record := Record{
				ID:          recordID,
				Kind:        artifacts[i].Kind,
				Name:        artifacts[i].Name,
				ContentType: contentTypeOrDefault(artifacts[i].ContentType),
				Payload:     append([]byte(nil), artifacts[i].Payload...),
				CreatedAt:   c.now().UTC(),
			}

			if _, err := c.config.RecordStore.Put(ctx, record); err != nil {
				return nil, fmt.Errorf("store artifact[%d]: %w", i, err)
			}
		}

		uri := artifacts[i].URI
		if uri == "" {
			uri = "sigil://record/" + recordID
		}

		refs = append(refs, ArtifactRef{
			Kind:        artifacts[i].Kind,
			Name:        artifacts[i].Name,
			ContentType: contentTypeOrDefault(artifacts[i].ContentType),
			RecordID:    recordID,
			URI:         uri,
		})
	}

	return refs, nil
}

func contentTypeOrDefault(contentType string) string {
	if contentType != "" {
		return contentType
	}

	return "application/json"
}
