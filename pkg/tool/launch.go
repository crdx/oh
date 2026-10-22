package tool

import "context"

type launchKey struct{}

func WithLaunch(ctx context.Context, launch func(context.Context) error) context.Context {
	return context.WithValue(ctx, launchKey{}, launch)
}

func Launched(ctx context.Context) error {
	launch, _ := ctx.Value(launchKey{}).(func(context.Context) error)
	if launch == nil {
		return nil
	}
	return launch(ctx)
}
