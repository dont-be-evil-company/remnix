package progress

import "context"

type Reporter func(string)

type key struct{}

func With(ctx context.Context, r Reporter) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, key{}, r)
}

func Report(ctx context.Context, msg string) {
	if ctx == nil || msg == "" {
		return
	}
	r, _ := ctx.Value(key{}).(Reporter)
	if r != nil {
		r(msg)
	}
}
