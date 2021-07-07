package internal

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
)

type At string

func In(directory string) At {
	return At(directory)
}

type Process struct {
	cmd    *exec.Cmd
	name   string
	logger Logger
}

func (inv At) Do(ctx context.Context, binary string, args ...string) *Process {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = string(inv)
	SetFlags(cmd)
	return &Process{cmd: cmd, name: binary, logger: SubLogger(ctx, binary)}
}

func (prc *Process) Env(environ map[string]string) *Process {
	prc.cmd.Env = os.Environ()
	for k, v := range environ {
		prc.cmd.Env = append(prc.cmd.Env, k+"="+v)
	}

	return prc
}

func (prc *Process) Text(data string) *Process {
	prc.cmd.Stdin = bytes.NewBufferString(data)
	return prc
}

func (prc *Process) Output() (string, error) {
	output := StreamingLogger(prc.logger)
	defer output.Close()
	var buffer bytes.Buffer
	prc.cmd.Stdout = io.MultiWriter(&buffer, output)
	prc.cmd.Stderr = output

	err := prc.cmd.Run()

	return strings.TrimSpace(buffer.String()), err
}

func (prc *Process) Exec() error {
	output := StreamingLogger(prc.logger)
	defer output.Close()
	prc.cmd.Stdout = output
	prc.cmd.Stderr = output

	return prc.cmd.Run()
}
