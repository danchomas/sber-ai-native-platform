package pgx

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/heinousness/aibi/internal/core/domain"
)

type Registry struct {
	local   *pgxpool.Pool
	sources Sources
	tmpl    PoolOptions

	mu    sync.RWMutex
	pools map[uuid.UUID]*pgxpool.Pool
}

type Sources interface {
	Get(ctx context.Context, id uuid.UUID) (*domain.Source, error)
}

func NewRegistry(local *pgxpool.Pool, sources Sources, tmpl PoolOptions) *Registry {
	return &Registry{
		local:   local,
		sources: sources,
		tmpl:    tmpl,
		pools:   make(map[uuid.UUID]*pgxpool.Pool),
	}
}

func (r *Registry) Local() *pgxpool.Pool { return r.local }

func (r *Registry) For(ctx context.Context, sourceID *uuid.UUID) (*pgxpool.Pool, error) {
	if sourceID == nil {
		return r.local, nil
	}

	r.mu.RLock()
	pool, ok := r.pools[*sourceID]
	r.mu.RUnlock()
	if ok {
		return pool, nil
	}

	src, err := r.sources.Get(ctx, *sourceID)
	if err != nil {
		return nil, err
	}
	return r.Open(ctx, src)
}

func (r *Registry) Open(ctx context.Context, src *domain.Source) (*pgxpool.Pool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if pool, ok := r.pools[src.ID]; ok {
		return pool, nil
	}

	opts := r.tmpl
	opts.DSN = src.DSN
	opts.AppName = "aibi-source-" + src.Name

	pool, err := NewPool(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("подключение к источнику %q: %w", src.Name, err)
	}
	r.pools[src.ID] = pool
	return pool, nil
}

func (r *Registry) Forget(id uuid.UUID) {
	r.mu.Lock()
	pool, ok := r.pools[id]
	delete(r.pools, id)
	r.mu.Unlock()

	if ok {
		pool.Close()
	}
}

func (r *Registry) Close() {
	r.mu.Lock()
	pools := make([]*pgxpool.Pool, 0, len(r.pools))
	for id, p := range r.pools {
		pools = append(pools, p)
		delete(r.pools, id)
	}
	r.mu.Unlock()

	for _, p := range pools {
		p.Close()
	}
}
