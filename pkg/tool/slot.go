package tool

import "context"

type queueKey struct{}

func WithQueue(ctx context.Context, enter func() func()) context.Context {
	return context.WithValue(ctx, queueKey{}, enter)
}

func TakeSlot(ctx context.Context, slots chan struct{}) error {
	select {
	case slots <- struct{}{}:
		return nil
	default:
	}
	leave := func() {}
	if enter, _ := ctx.Value(queueKey{}).(func() func()); enter != nil {
		leave = enter()
	}
	defer leave()
	select {
	case slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
