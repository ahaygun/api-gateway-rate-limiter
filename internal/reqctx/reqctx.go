// Package reqctx carries per-request info that is filled in by different
// layers of the middleware chain (the router sets the route, auth sets the
// client) and read back by logging and metrics once the request is handled.
package reqctx

import "context"

type ctxKey struct{}

// Info is a mutable holder attached to the request context. Layers write to
// it as the request flows inward; outer middleware read it on the way out.
type Info struct {
	RequestID string
	Route     string // matched route prefix, or "" if unmatched
	Client    string // authenticated client name, or "" if anonymous
	Plan      string // client's plan, or "" if anonymous
}

// With attaches a fresh Info holder to ctx and returns both.
func With(ctx context.Context) (context.Context, *Info) {
	info := &Info{}
	return context.WithValue(ctx, ctxKey{}, info), info
}

// From returns the Info holder attached to ctx, or nil if none is present.
func From(ctx context.Context) *Info {
	info, _ := ctx.Value(ctxKey{}).(*Info)
	return info
}
