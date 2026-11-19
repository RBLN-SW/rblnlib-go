// Package command implements bounded, context-aware execution without a shell.
package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	system "github.com/rbln-sw/rblnlib-go/pkg/system/v1"
)

type buffer struct {
	data     bytes.Buffer
	limit    int
	overflow bool
}

func (b *buffer) Write(p []byte) (int, error) {
	n := len(p)
	left := b.limit - b.data.Len()
	if n > left {
		b.overflow = true
		p = p[:left]
	}
	_, _ = b.data.Write(p)
	return n, nil
}

// Run collects bounded output from a command constructed with ctx.
func Run(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Allow a bounded drain if a descendant inherited an output pipe.
	cmd.WaitDelay = 250 * time.Millisecond
	stdout := &buffer{limit: 16 * 1024 * 1024}
	stderr := &buffer{limit: 4096}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		kind := system.Unavailable
		if errors.Is(err, exec.ErrNotFound) {
			kind = system.Unsupported
		}
		return nil, &system.Error{Kind: kind, Op: cmd.Args[0], Message: stderr.data.String(), Cause: err}
	}
	if stdout.overflow {
		return nil, &system.Error{Kind: system.InvalidData, Op: cmd.Args[0], Message: fmt.Sprintf("output exceeds %d bytes", stdout.limit)}
	}
	return stdout.data.Bytes(), nil
}
