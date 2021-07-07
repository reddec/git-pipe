package internal

import (
	"bufio"
	"context"
	"io"
	"log"
	"os"
)

type Logger interface {
	Println(...interface{})
}

type LoggerFunc func(...interface{})

func (lf LoggerFunc) Println(vars ...interface{}) {
	lf(vars...)
}

func Namespaced(parent Logger, name string) Logger {
	return LoggerFunc(func(values ...interface{}) {
		var cp = make([]interface{}, len(values)+1)
		cp[0] = "[" + name + "]"
		copy(cp[1:], values)
		parent.Println(cp...)
	})
}

func StreamingLogger(logger Logger) io.WriteCloser {
	r, w := io.Pipe()

	go func() {
		defer w.Close()
		defer r.Close()

		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			logger.Println(scanner.Text())
		}
	}()

	return w
}

type loggerKeyType string

const (
	ctxLogger loggerKeyType = "logger"
)

func WithLogger(ctx context.Context, logger Logger) context.Context {
	return context.WithValue(ctx, ctxLogger, logger)
}

func LoggerFromContext(ctx context.Context) Logger {
	if v, ok := ctx.Value(ctxLogger).(Logger); ok {
		return v
	}
	return log.New(os.Stderr, "", log.LstdFlags)
}

func SubLogger(ctx context.Context, name string) Logger {
	return Namespaced(LoggerFromContext(ctx), name)
}
