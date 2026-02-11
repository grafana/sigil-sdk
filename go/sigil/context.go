package sigil

import "context"

type contextKey struct{}

// WithConversationID stores a conversation ID in the context.
// StartGeneration and StartToolExecution read it when the explicit field is empty.
func WithConversationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// ConversationIDFromContext retrieves the conversation ID stored by WithConversationID.
func ConversationIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(contextKey{}).(string)
	return id, ok && id != ""
}
