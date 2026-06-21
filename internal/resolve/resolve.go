// Package resolve answers queries against an index through swappable resolvers.
// The keyword resolver needs no LLM; later resolvers may add a local or, under
// a permissive policy, a remote model.
package resolve

import (
	"context"

	"github.com/dcadolph/whodar/internal/index"
	"github.com/dcadolph/whodar/internal/model"
)

// Resolver answers a natural-language query with ranked matches.
type Resolver interface {
	// Resolve ranks up to limit people for query.
	Resolve(ctx context.Context, query string, limit int) ([]model.Match, error)
}

// ResolverFunc adapts a function to the Resolver interface.
type ResolverFunc func(ctx context.Context, query string, limit int) ([]model.Match, error)

// Resolve calls f.
func (f ResolverFunc) Resolve(ctx context.Context, query string, limit int) ([]model.Match, error) {
	return f(ctx, query, limit)
}

// Keyword is a non-LLM resolver that ranks matches by keyword and affinity
// scoring over the index. It needs no external services.
type Keyword struct {
	// ix is the index to search.
	ix *index.Index
}

// NewKeyword returns a Keyword resolver over ix. It panics if ix is nil.
func NewKeyword(ix *index.Index) *Keyword {
	if ix == nil {
		panic("resolve: NewKeyword requires a non-nil index")
	}
	return &Keyword{ix: ix}
}

// Resolve ranks people for query using the index. The context is unused; it is
// present to satisfy the Resolver interface that LLM resolvers will honor.
func (k *Keyword) Resolve(_ context.Context, query string, limit int) ([]model.Match, error) {
	return k.ix.Search(query, limit), nil
}
