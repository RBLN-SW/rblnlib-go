package smi

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rbln-sw/rblnlib-go/pkg/device"
	system "github.com/rbln-sw/rblnlib-go/pkg/system/v1"
)

const a = "0000:01:00.0"
const b = "0000:02:00.0"

func smiFixture(t *testing.T) *Client {
	t.Helper()
	c, err := New(Options{Binary: "test-smi", LockPath: filepath.Join(t.TempDir(), "rsd.lock")})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func payload(address string, index any, group int, memory string) string {
	return fmt.Sprintf(`{"device":"rbln%s","name":"RBLN-Test","npu":%v,"group_id":"%d","pci":{"bus_id":%q,"dev":"0x1220","numa_node":-1,"link_width":"16"},"memory":{"total":%q},"status":"normal"}`, address[5:7], index, group, address, memory)
}

func TestInventoryScalarCompatibilityAndPresence(t *testing.T) {
	for _, index := range []string{"0", `"0"`} {
		c := smiFixture(t)
		c.run = func(context.Context, string, ...string) ([]byte, error) {
			return []byte(`{"KMD_version":"4.0","devices":[` + payload(a, index, 0, "9007199254740993") + `]}`), nil
		}
		s, err := c.ListDevices(context.Background())
		if err != nil || len(s.Devices) != 1 {
			t.Fatalf("inventory: %+v %v", s, err)
		}
		d := s.Devices[0]
		if d.Index == nil || *d.Index != 0 || d.MemoryTotalBytes == nil || *d.MemoryTotalBytes != 9007199254740993 || d.State != system.StateUnknown {
			t.Fatalf("value contract: %+v", d)
		}
	}
	c := smiFixture(t)
	c.run = func(context.Context, string, ...string) ([]byte, error) {
		return []byte(`{"devices":[` + payload(a, 0, 0, "N/A") + `]}`), nil
	}
	s, err := c.ListDevices(context.Background())
	if err != nil || s.Devices[0].MemoryTotalBytes != nil || s.Err() == nil {
		t.Fatalf("unavailable memory became zero: %+v %v", s, err)
	}
	for _, data := range []string{`{}`, `{"devices":null}`, `not json`} {
		c.run = func(context.Context, string, ...string) ([]byte, error) { return []byte(data), nil }
		if _, err := c.ListDevices(context.Background()); !errors.Is(err, system.ErrInvalidData) {
			t.Fatalf("invalid JSON accepted: %q %v", data, err)
		}
	}
}

func TestRSDUsesStablePCISelectorsAndVerifiesExactMembership(t *testing.T) {
	c := smiFixture(t)
	created := false
	var commands [][]string
	c.run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) == 2 && args[0] == "group" && args[1] == "--help" {
			return []byte("Attach devices using PCI addresses or device index"), nil
		}
		switch strings.Join(args, " ") {
		case "-j":
			return []byte(`{"devices":[` + payload(a, 5, 0, "8") + `,` + payload(b, 9, 0, "8") + `]}`), nil
		case "-g -j":
			group := 0
			if created {
				group = 1
			}
			return []byte(`{"devices":[` + payload(a, 0, group, "8") + `,` + payload(b, 1, group, "8") + `]}`), nil
		default:
			commands = append(commands, append([]string(nil), args...))
			created = true
			return nil, nil
		}
	}
	g, err := c.RecreateGroup(context.Background(), []string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if g.ID != 1 || g.Node.Path != "/dev/rsd1" || !reflect.DeepEqual(commands, [][]string{{"group", "-c", "1", "-a", a + "," + b}}) {
		t.Fatalf("stable selector mismatch: %+v %v", g, commands)
	}
}

func TestRSDRefusesPartialSelectionBeforeMutation(t *testing.T) {
	for _, missing := range []bool{false, true} {
		c := smiFixture(t)
		c.run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if len(args) == 2 && args[0] == "group" && args[1] == "--help" {
				return []byte("Attach devices using PCI addresses or device index"), nil
			}
			if args[0] == "group" {
				t.Fatal("must not mutate")
			}
			if missing {
				return []byte(`{"devices":[]}`), nil
			}
			return []byte(`{"devices":[` + payload(a, 0, 2, "8") + `,` + payload(b, 1, 2, "8") + `]}`), nil
		}
		_, err := c.RecreateGroup(context.Background(), []string{a})
		want := system.ErrConflict
		if missing {
			want = system.ErrNotReady
		}
		if !errors.Is(err, want) {
			t.Fatalf("missing=%v: %v", missing, err)
		}
	}
}

func TestRSDReadbackFailureAndCommandErrorNeverReplay(t *testing.T) {
	for _, commandFails := range []bool{true, false} {
		c := smiFixture(t)
		writes := 0
		c.run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if len(args) == 2 && args[0] == "group" && args[1] == "--help" {
				return []byte("Attach devices using PCI addresses or device index"), nil
			}
			if args[0] == "group" {
				writes++
				if commandFails {
					return nil, context.DeadlineExceeded
				}
				return nil, nil
			}
			return []byte(`{"devices":[` + payload(a, 5, 0, "8") + `]}`), nil
		}
		_, err := c.RecreateGroup(context.Background(), []string{a})
		if !errors.Is(err, system.ErrIndeterminate) || writes != 1 {
			t.Fatalf("uncertain write replayed: %v writes=%d", err, writes)
		}
	}
}

func TestLegacyPCIAttributesKeepSMISpelling(t *testing.T) {
	c := smiFixture(t)
	c.run = func(context.Context, string, ...string) ([]byte, error) {
		return []byte(`{"devices":[{"device":"rbln0","pci":{"dev":"0x1220","bus_id":"0000:AB:00.0","numa_node":"-1","link_speed":"16GT/s","link_width":"x16"}}]}`), nil
	}
	s, err := c.ListDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	d := s.Devices[0]
	if !d.DeviceIDKnown || d.DeviceID != 0x1220 || d.PCIAddress != "0000:ab:00.0" || d.Topology.LinkWidth == nil || *d.Topology.LinkWidth != 16 {
		t.Fatalf("normalized PCI: %+v", d)
	}
	legacy := device.FromSystem(d)
	if legacy.PCIDeviceID != "0x1220" || legacy.PCIBusID != "0000:AB:00.0" || legacy.PCINumaNode != "-1" || legacy.PCILinkSpeed != "16GT/s" || legacy.PCILinkWidth != "x16" {
		t.Fatalf("legacy selector spelling changed: %+v", legacy)
	}
}
