package repository

import "context"

type occurrenceKey struct{}

func WithOccurrence(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, occurrenceKey{}, key)
}
func Occurrence(ctx context.Context) string { v, _ := ctx.Value(occurrenceKey{}).(string); return v }
