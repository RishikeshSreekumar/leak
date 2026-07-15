package cmd

import "context"

// contextWithDeps returns a child context carrying the dependency bundle.
func contextWithDeps(ctx context.Context, d *Deps) context.Context {
	return context.WithValue(ctx, depsKey{}, d)
}
