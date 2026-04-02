package auth

import "context"

type contextKey struct{}

// ContextWithUserID returns a new context carrying the authenticated user ID.
func ContextWithUserID(ctx context.Context, userID uint) context.Context {
	return context.WithValue(ctx, contextKey{}, userID)
}

// UserIDFromContext extracts the authenticated user ID from the context.
func UserIDFromContext(ctx context.Context) (uint, bool) {
	id, ok := ctx.Value(contextKey{}).(uint)
	return id, ok
}
