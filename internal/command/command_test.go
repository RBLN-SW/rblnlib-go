package command

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	system "github.com/rbln-sw/rblnlib-go/pkg/system/v1"
)

func TestHelperProcess(t *testing.T) {
	if os.Getenv("RBLNLIB_COMMAND_HELPER") != "1" {
		return
	}
	if os.Args[len(os.Args)-1] == "stderr" {
		_, _ = fmt.Fprint(os.Stderr, strings.Repeat("x", 4097))
		os.Exit(1)
	}
	if os.Args[len(os.Args)-1] == "wait" {
		time.Sleep(time.Minute)
	}
	_, _ = fmt.Fprint(os.Stdout, `{"devices":[]}`)
	_, _ = fmt.Fprint(os.Stderr, "diagnostic on stderr")
	os.Exit(0)
}
func TestSeparatedOutputAndDeadline(t *testing.T) {
	t.Setenv("RBLNLIB_COMMAND_HELPER", "1")
	out, err := Run(context.Background(), os.Args[0], "-test.run=TestHelperProcess", "--", "json")
	if err != nil || string(out) != `{"devices":[]}` {
		t.Fatalf("stdout polluted: %q %v", out, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if _, err := Run(ctx, os.Args[0], "-test.run=TestHelperProcess", "--", "wait"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline: %v", err)
	}
	b := &buffer{limit: 3}
	n, err := b.Write([]byte(strings.Repeat("x", 12)))
	if err != nil || n != 12 || b.data.String() != "xxx" || !b.overflow {
		t.Fatalf("bounded output: %d %q %v", n, b.data.String(), err)
	}
}

func TestBoundedCommandStderr(t *testing.T) {
	t.Setenv("RBLNLIB_COMMAND_HELPER", "1")
	_, err := Run(context.Background(), os.Args[0], "-test.run=TestHelperProcess", "--", "stderr")
	var e *system.Error
	if !errors.As(err, &e) || len(e.Message) != 4096 {
		t.Fatal("expected a bounded stderr diagnostic")
	}
}

func TestCommandDeadlineWithInheritedPipe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Run(ctx, "sh", "-c", "sleep 3 & wait")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatalf("inherited pipe delayed return: %s, %v", time.Since(start), err)
	}
}
