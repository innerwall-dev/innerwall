package authn

import "context"

// Principal is the authenticated operator as the domain layer sees it. Both
// operator credential forms resolve to it, and the command line acts as it;
// it carries no record of which credential, or which transport, produced
// it. DisplayName is the optional name set with the password; a surface
// renders a fallback when it is nil.
type Principal struct {
	DisplayName *string
}

type principalKey struct{}

// NewContext returns a context carrying p. The surface's authentication
// middleware and the command line's entry path are its callers; nothing
// else places a principal.
func NewContext(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// FromContext returns the principal ctx carries. It reports false when
// there is none, so a caller that reaches the domain layer without having
// been authenticated is told so rather than handed a zero principal.
func FromContext(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(*Principal)
	if !ok || p == nil {
		return nil, false
	}
	return p, true
}
