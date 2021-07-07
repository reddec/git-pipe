//+build !linux

package internal

import "os/exec"

// you should suffer without linux.
func SetFlags(cmd *exec.Cmd) {}
