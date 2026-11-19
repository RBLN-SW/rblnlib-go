package host

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/rbln-sw/rblnlib-go/pkg/device"
	system "github.com/rbln-sw/rblnlib-go/pkg/system/v1"
	"golang.org/x/sys/unix"
)

func TestVFIONodesAndIOMMUMembership(t *testing.T) {
	c := fixture(t)
	p := addPCI(t, c, pf, system.VFIODriver)
	g := filepath.Join(c.opts.SysRoot, "kernel/iommu_groups/17")
	link(t, p, filepath.Join(g, "devices", pf))
	link(t, g, filepath.Join(p, "iommu_group"))
	link(t, "/dev/null", filepath.Join(c.opts.DevRoot, "vfio/vfio"))
	link(t, "/dev/null", filepath.Join(c.opts.DevRoot, "vfio/17"))
	id, members, err := c.IOMMUGroup(context.Background(), pf)
	if err != nil || id != 17 || !reflect.DeepEqual(members, []string{pf}) {
		t.Fatalf("IOMMU %d %v %v", id, members, err)
	}
	nodes, err := c.DeviceNodes(context.Background(), pf)
	if err != nil || len(nodes) != 2 || nodes[0].Path != "/dev/vfio/vfio" || nodes[1].Path != "/dev/vfio/17" {
		t.Fatalf("VFIO nodes %+v %v", nodes, err)
	}
	other := addPCI(t, c, vf1, system.VFIODriver)
	link(t, other, filepath.Join(g, "devices", vf1))
	_, members, err = c.IOMMUGroup(context.Background(), pf)
	if err != nil || len(members) != 2 {
		t.Fatalf("library must report shared groups, not select policy: %v %v", members, err)
	}
}

func TestProbeHostDriverCommand(t *testing.T) {
	for _, tc := range []struct {
		program string
		opts    Options
		want    string
	}{
		{"rbln-smi", Options{}, "<--version>"},
		{"chroot", Options{HostRoot: "/host root"}, "</host root><rbln-smi><--version>"},
		{"nsenter", Options{HostPID: 42}, "<--target><42><--mount><--uts><--ipc><--><env><TMPDIR=/tmp><rbln-smi><--version>"},
	} {
		t.Run(tc.program, func(t *testing.T) {
			bin := t.TempDir()
			t.Setenv("PATH", bin)
			if err := os.WriteFile(filepath.Join(bin, tc.program), []byte("#!/bin/sh\nprintf '<%s>' \"$@\"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			c, err := New(tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			got, err := c.ProbeHostDriver(context.Background())
			if err != nil || got != tc.want {
				t.Fatalf("probe = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestModuleObservationAndBusyRemoval(t *testing.T) {
	c := fixture(t)
	m, err := c.Module(context.Background(), "rebellions")
	if err != nil || m.Loaded {
		t.Fatalf("absent module %+v %v", m, err)
	}
	p := filepath.Join(c.opts.SysRoot, "module/rebellions")
	put(t, filepath.Join(p, "version"), "4.0.0")
	put(t, filepath.Join(p, "initstate"), "live")
	put(t, filepath.Join(p, "refcnt"), "3")
	m, err = c.Module(context.Background(), "rebellions")
	if err != nil || !m.Loaded || m.RefCount == nil || *m.RefCount != 3 || m.Version != "4.0.0" {
		t.Fatalf("module %+v %v", m, err)
	}
	calls := 0
	c.unload = func(name string) error {
		calls++
		if name != "rebellions" {
			t.Fatal(name)
		}
		return unix.EBUSY
	}
	if err := c.UnloadModule(context.Background(), "rebellions"); !errors.Is(err, system.ErrBusy) || calls != 1 {
		t.Fatalf("busy unload %v calls=%d", err, calls)
	}
	if err := c.UnloadModule(context.Background(), "--force"); !errors.Is(err, system.ErrInvalidArgument) || calls != 1 {
		t.Fatalf("invalid module %v", err)
	}
	c.unload = func(string) error { return unix.ENOENT }
	if err := c.UnloadModule(context.Background(), "rebellions"); err != nil {
		t.Fatal(err)
	}
}

func TestUnmountProgressAndRDS(t *testing.T) {
	c := fixture(t)
	file := filepath.Join(c.opts.ProcRoot, "self/mountinfo")
	put(t, file, "10 1 0:1 / /driver rw - tmpfs tmpfs rw\n")
	c.unmount = func(string) error { return nil }
	if err := c.UnmountRootfs(context.Background(), "/driver"); !errors.Is(err, system.ErrNotReady) {
		t.Fatalf("no-op unmount spun or passed: %v", err)
	}
	c.unmount = func(path string) error {
		if path != "/driver" {
			t.Fatal(path)
		}
		put(t, file, "")
		return nil
	}
	if err := c.UnmountRootfs(context.Background(), "/driver"); err != nil {
		t.Fatal(err)
	}
	if err := c.UnmountRootfs(context.Background(), "/"); !errors.Is(err, system.ErrInvalidArgument) {
		t.Fatal(err)
	}
	p := addPCI(t, c, pf, "rblnfs")
	driver := filepath.Join(c.pciRoot(), "drivers/rblnfs")
	link(t, p, filepath.Join(driver, pf))
	put(t, filepath.Join(p, "driver_override"), "rblnfs")
	bound, err := c.RDSBindings(context.Background())
	if err != nil || !reflect.DeepEqual(bound, []string{pf}) {
		t.Fatalf("RDS %v %v", bound, err)
	}
	var writes []string
	c.write = func(_ context.Context, path, value string) error {
		writes = append(writes, filepath.Base(path))
		if filepath.Base(path) == "unbind" {
			return os.Remove(filepath.Join(p, "driver"))
		}
		if filepath.Base(path) == "driver_override" {
			put(t, path, value)
		}
		return nil
	}
	if err := c.RestoreRDS(context.Background(), pf); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(writes, []string{"unbind", "driver_override", "drivers_probe"}) {
		t.Fatalf("RDS sequence %v", writes)
	}
	link(t, "/dev/null", filepath.Join(c.opts.DevRoot, "rblnfs0"))
	nodes, err := c.RDSNodes(context.Background())
	if err != nil || len(nodes) != 1 || nodes[0].Kind != "rds" {
		t.Fatalf("RDS nodes %+v %v", nodes, err)
	}
}

func TestNativeOrdinalAndLocalEnrichment(t *testing.T) {
	c := fixture(t)
	addPCI(t, c, pf, system.NativeDriver)
	addNative(t, c, pf, "rbln7", "accel", "accel/accel19", 4)
	identity, err := c.NativePF(context.Background(), pf)
	if err != nil || identity.Index != 7 || identity.Name != "rbln7" {
		t.Fatalf("native PF %+v %v", identity, err)
	}
	meta := system.Snapshot{Source: "smi/json", Devices: []system.Device{{PCIAddress: pf, Name: "rbln7", UUID: "function-uuid", State: system.StateReady, Index: system.Pointer(uint32(2)), Compute: system.Pointer(true)}}}
	s, err := c.Enrich(context.Background(), meta)
	if err != nil || s.Devices[0].State != system.StateFault || s.Devices[0].Index == nil || *s.Devices[0].Index != 2 || s.Devices[0].Nodes[0].Path != "/dev/accel/accel19" {
		t.Fatalf("cached metadata overrode live facts: %+v %v", s, err)
	}
	meta.Devices[0].UUID = "different-function"
	s, err = c.Enrich(context.Background(), meta)
	if err != nil || s.Devices[0].Index != nil || !errors.Is(s.Err(), system.ErrNotReady) {
		t.Fatalf("stale identity joined %+v %v", s, err)
	}
}

func TestEnrichRetainsReportedPCIAndMetadataDiagnostics(t *testing.T) {
	c := fixture(t)
	addPCI(t, c, pf, system.NativeDriver)
	addNative(t, c, pf, "rbln7", "rebellions", "rbln7", 4)
	m := system.Device{PCIAddress: pf, Name: "rbln7", State: system.StateReady,
		ReportedPCI: &system.ReportedPCI{Address: pf, DeviceID: "0x1220", LinkWidth: system.Pointer("x16")},
		Issues:      []system.Issue{{Field: "index", Err: system.ErrInvalidData}, {Field: "state", Err: system.ErrUnsupported}},
	}
	s, err := c.Enrich(context.Background(), system.Snapshot{Devices: []system.Device{m}})
	if err != nil || len(s.Devices) != 1 {
		t.Fatalf("enrich: %+v, %v", s, err)
	}
	d := s.Devices[0]
	projected := device.FromSystem(d)
	if projected.PCIDeviceID != "0x1220" || projected.PCILinkWidth != "x16" || d.State != system.StateFault {
		t.Errorf("reported PCI or live state lost: %+v %s", projected, d.State)
	}
	if !errors.Is(s.Err(), system.ErrInvalidData) || errors.Is(s.Err(), system.ErrUnsupported) {
		t.Errorf("wrong issue provenance: %v", s.Err())
	}
}

func TestBindingLockErrorsShareClassification(t *testing.T) {
	c := fixture(t)
	c.opts.LockPath = filepath.Join(t.TempDir(), "missing", "lock")
	req := system.BindingRequest{PCIAddress: pf, Driver: system.NativeDriver}
	for _, op := range []func(context.Context, system.BindingRequest) error{c.Bind, c.Unbind} {
		if err := op(context.Background(), req); !errors.Is(err, system.ErrNotFound) || !errors.Is(err, os.ErrNotExist) {
			t.Errorf("lock error lost SDK or OS classification: %v", err)
		}
	}
}
