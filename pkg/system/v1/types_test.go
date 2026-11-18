package system

import (
	"context"
	"errors"
	"testing"
)

func TestErrorKindsAndPartialResult(t *testing.T) {
	err := &Error{Kind: Indeterminate, Cause: context.DeadlineExceeded, Target: "rbln0"}
	if !errors.Is(err, ErrIndeterminate) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	r := CommandResult{Outcomes: []Outcome{{Target: "a"}, {Target: "b", Err: ErrBusy}}}
	if !errors.Is(r.Err(), ErrBusy) {
		t.Fatal(r.Err())
	}
	s := Snapshot{Devices: []Device{{Issues: []Issue{{Field: "state", Err: ErrUnsupported}}}}}
	if !errors.Is(s.Err(), ErrUnsupported) {
		t.Fatal(s.Err())
	}
}
func TestIdentityAndTargets(t *testing.T) {
	for _, s := range []string{"../../etc", "0000:00:20.0", "0000:00:01.8", "0000:00:01"} {
		if _, err := NormalizePCIAddress(s); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("accepted %q", s)
		}
	}
	if s, err := NormalizePCIAddress("ABCD:EF:1F.7"); err != nil || s != "abcd:ef:1f.7" {
		t.Fatalf("hex domain %s %v", s, err)
	}
	for _, targets := range [][]string{nil, {""}, {"a", "a"}, {"../a"}, {"a\n"}} {
		if ValidTargets(targets) == nil {
			t.Fatalf("accepted %v", targets)
		}
	}
}

func TestVMDPCIAddress(t *testing.T) {
	if got, err := NormalizePCIAddress("10000:AB:02.1"); err != nil || got != "10000:ab:02.1" {
		t.Fatalf("VMD address: %q %v", got, err)
	}
}
