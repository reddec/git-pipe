package noregister

import "context"

type NoRegister struct{}

func (nr *NoRegister) Register(_ context.Context, _ []string) error {
	return nil
}
