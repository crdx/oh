package tasks

import "context"

type managedKey struct{}

func Managed(ctx context.Context) bool {
	isManaged, _ := ctx.Value(managedKey{}).(bool)
	return isManaged
}

func markManaged(ctx context.Context) context.Context {
	return context.WithValue(ctx, managedKey{}, true)
}
