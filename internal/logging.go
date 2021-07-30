package internal

import (
	"bufio"
	"context"
	"io"

	"go.uber.org/zap"
)

func StreamingLogger(logger *zap.Logger) io.WriteCloser {
	r, w := io.Pipe()

	go func() {
		defer w.Close()
		defer r.Close()

		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			logger.Debug(scanner.Text())
		}
	}()

	return w
}

type loggerKeyType string

const (
	ctxLogger loggerKeyType = "logger"
)

func WithLogger(ctx context.Context, logger *zap.Logger) context.Context {
	return context.WithValue(ctx, ctxLogger, logger)
}

func LoggerFromContext(ctx context.Context) *zap.Logger {
	if v, ok := ctx.Value(ctxLogger).(*zap.Logger); ok {
		return v
	}
	return zap.L()
}

func SubLogger(ctx context.Context, name string) *zap.Logger {
	return LoggerFromContext(ctx).Named(name)
}

func WithSubLogger(ctx context.Context, name string) context.Context {
	return WithLogger(ctx, SubLogger(ctx, name))
}
