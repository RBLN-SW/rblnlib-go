package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	system "github.com/rbln-sw/rblnlib-go/pkg/system/v1"
	"golang.org/x/sys/unix"
)

const pf = "0000:01:00.0"
const vf1 = "0000:01:00.1"
const vf2 = "0000:01:00.2"

func put(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}
func link(t *testing.T, target, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}
func fixture(t *testing.T) *Client {
	t.Helper()
	root := t.TempDir()
	c, err := New(Options{SysRoot: filepath.Join(root, "sys"), DevRoot: filepath.Join(root, "dev"), ProcRoot: filepath.Join(root, "proc"), LockPath: filepath.Join(root, "host.lock"), CommandTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{c.opts.DevRoot, c.opts.ProcRoot, filepath.Join(c.pciRoot(), "devices")} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return c
}
func addPCI(t *testing.T, c *Client, address, driver string) string {
	t.Helper()
	p := filepath.Join(c.opts.SysRoot, "devices/pci0000:00/0000:00:01.0", address)
	for name, value := range map[string]string{"vendor": "0x1eff", "class": "0x120000", "device": "0x1220", "driver_override": "\n", "numa_node": "-1"} {
		put(t, filepath.Join(p, name), value)
	}
	link(t, p, filepath.Join(c.pciRoot(), "devices", address))
	if driver != "" {
		d := filepath.Join(c.pciRoot(), "drivers", driver)
		put(t, filepath.Join(d, "bind"), "")
		put(t, filepath.Join(d, "unbind"), "")
		link(t, d, filepath.Join(p, "driver"))
	}
	return p
}
func addNative(t *testing.T, c *Client, address, name, class, node string, status int) string {
	t.Helper()
	p, _ := c.devicePath(address)
	entry := name
	if class == "accel" {
		entry = filepath.Base(node)
	}
	base := filepath.Join(c.opts.SysRoot, "class", class, entry)
	for attr, value := range map[string]string{"name": name, "pci_addr": address, "status": fmt.Sprint(status), "kernel_version": "4.0.0", "fw_ver": "1.2.3", "smc_ver": "1.0", "card_name": "RBLN-Test", "unique_id": "function-uuid", "serial_id": "board-serial", "dram_total": "8589934592", "uevent": "DEVNAME=" + node + "\n"} {
		put(t, filepath.Join(base, attr), value)
	}
	link(t, p, filepath.Join(base, "device"))
	link(t, "/dev/null", filepath.Join(c.opts.DevRoot, node))
	var st unix.Stat_t
	if err := unix.Stat("/dev/null", &st); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(base, "dev"), fmt.Sprintf("%d:%d", unix.Major(uint64(st.Rdev)), unix.Minor(uint64(st.Rdev))))
	return base
}

func TestLegacyAndAccelUseSameContract(t *testing.T) {
	for _, tc := range []struct{ class, node string }{{"rebellions", "rbln7"}, {"accel", "accel/accel19"}, {"accel", "accel19"}} {
		t.Run(tc.class+"/"+tc.node, func(t *testing.T) {
			c := fixture(t)
			addPCI(t, c, pf, system.NativeDriver)
			addNative(t, c, pf, "rbln7", tc.class, tc.node, 0)
			d, err := c.GetDevice(context.Background(), pf)
			if err != nil {
				t.Fatal(err)
			}
			if d.Name != "rbln7" || d.State != system.StateReady || len(d.Nodes) != 1 || d.Nodes[0].Path != "/dev/"+tc.node || d.Nodes[0].HostPath != filepath.Join(c.opts.DevRoot, tc.node) {
				t.Fatalf("wrong identity/path/state: %+v", d)
			}
			if d.MemoryTotalBytes == nil || *d.MemoryTotalBytes != 8589934592 || d.Topology.NUMANode != nil || d.Topology.PCIeRoot != "pci0000:00" {
				t.Fatalf("wrong normalized metadata: %+v", d)
			}
		})
	}
}

func TestAccelStateIsIndependentOfNodeNumber(t *testing.T) {
	c := fixture(t)
	addPCI(t, c, pf, system.NativeDriver)
	legacy := addNative(t, c, pf, "rbln7", "rebellions", "rbln7", 0)
	accel := addNative(t, c, pf, "rbln7", "accel", "accel/accel19", 4)
	_ = legacy
	s, err := c.ReadState(context.Background(), pf)
	if err != nil || s != system.StateFault {
		t.Fatalf("got %s %v", s, err)
	}
	put(t, filepath.Join(accel, "status"), "99")
	s, err = c.ReadState(context.Background(), pf)
	if s != system.StateUnknown || !errors.Is(err, system.ErrUnsupported) {
		t.Fatalf("future status became known: %s %v", s, err)
	}
	put(t, filepath.Join(accel, "dev"), "999:999")
	if _, err := c.DeviceNodes(context.Background(), pf); !errors.Is(err, system.ErrNotReady) {
		t.Fatalf("stale dev_t accepted: %v", err)
	}
	put(t, filepath.Join(accel, "uevent"), "DEVNAME=../../etc/passwd")
	if _, err := c.DeviceNodes(context.Background(), pf); !errors.Is(err, system.ErrInvalidData) {
		t.Fatalf("unsafe DEVNAME accepted: %v", err)
	}
}

func partition(t *testing.T, c *Client) {
	t.Helper()
	p := addPCI(t, c, pf, system.NativeDriver)
	put(t, filepath.Join(p, "sriov_numvfs"), "2")
	put(t, filepath.Join(p, "sriov_totalvfs"), "4")
	for i, a := range []string{vf1, vf2} {
		v := addPCI(t, c, a, "")
		link(t, p, filepath.Join(v, "physfn"))
		link(t, v, filepath.Join(p, fmt.Sprintf("virtfn%d", i)))
	}
}

func TestSRIOVTopologyAndTwoVFs(t *testing.T) {
	c := fixture(t)
	partition(t, c)
	info, err := c.ReadSRIOV(context.Background(), vf1)
	if err != nil || info.Kind != system.VirtualFunction || info.ParentPCIAddress != pf || info.ParentEnabledVFs == nil || *info.ParentEnabledVFs != 2 || info.EnabledVFs != 0 {
		t.Fatalf("VF: %+v %v", info, err)
	}
	info, err = c.ReadSRIOV(context.Background(), pf)
	if err != nil || !reflect.DeepEqual(info.VirtualFunctions, []string{vf1, vf2}) || info.EnabledVFs != 2 {
		t.Fatalf("PF: %+v %v", info, err)
	}
	p, _ := c.devicePath(pf)
	put(t, filepath.Join(p, "sriov_numvfs"), "4")
	if _, err := c.ReadSRIOV(context.Background(), pf); !errors.Is(err, system.ErrNotReady) {
		t.Fatalf("incomplete topology accepted: %v", err)
	}
	put(t, filepath.Join(p, "sriov_numvfs"), "-1")
	if _, err := c.ReadSRIOV(context.Background(), pf); !errors.Is(err, system.ErrInvalidData) {
		t.Fatalf("negative count accepted: %v", err)
	}
}

func TestAbsentAndDanglingAreDistinct(t *testing.T) {
	c := fixture(t)
	p := addPCI(t, c, pf, "")
	info, err := c.ReadSRIOV(context.Background(), pf)
	if err != nil || info.Supported || info.Kind != system.PhysicalFunction {
		t.Fatalf("older PF: %+v %v", info, err)
	}
	if driver, err := c.CurrentDriver(context.Background(), pf); err != nil || driver != "" {
		t.Fatalf("unbound: %s %v", driver, err)
	}
	link(t, filepath.Join(c.pciRoot(), "devices", vf1), filepath.Join(p, "physfn"))
	if info, err := c.ReadSRIOV(context.Background(), pf); err == nil || info.Kind == system.PhysicalFunction {
		t.Fatalf("dangling VF became PF: %+v %v", info, err)
	}
	link(t, filepath.Join(c.pciRoot(), "drivers/missing"), filepath.Join(p, "driver"))
	if _, err := c.CurrentDriver(context.Background(), pf); err == nil {
		t.Fatal("dangling driver became unbound")
	}
	if _, err := c.GetDevice(context.Background(), vf2); !errors.Is(err, system.ErrNotFound) {
		t.Fatalf("missing device: %v", err)
	}
}

func TestPartialInventoryAndInvalidMemory(t *testing.T) {
	c := fixture(t)
	addPCI(t, c, pf, system.NativeDriver)
	class := addNative(t, c, pf, "rbln0", "rebellions", "rbln0", 0)
	put(t, filepath.Join(class, "dram_total"), "not-bytes")
	p := addPCI(t, c, vf1, "")
	if err := os.Remove(filepath.Join(p, "vendor")); err != nil {
		t.Fatal(err)
	}
	s, err := c.ListDevices(context.Background())
	if err != nil || len(s.Devices) != 1 || len(s.Issues) != 1 || s.Err() == nil || s.Devices[0].MemoryTotalBytes != nil {
		t.Fatalf("partial snapshot lost: %+v %v", s, err)
	}
}

func TestBindingVerifiesStateAndStopsAfterFailure(t *testing.T) {
	c := fixture(t)
	p := addPCI(t, c, pf, system.VFIODriver)
	native := filepath.Join(c.pciRoot(), "drivers", system.NativeDriver)
	put(t, filepath.Join(native, "bind"), "")
	var writes []string
	c.write = func(ctx context.Context, path, value string) error {
		writes = append(writes, filepath.Base(path)+"="+value)
		switch filepath.Base(path) {
		case "driver_override":
			put(t, path, value)
		case "unbind":
			return os.Remove(filepath.Join(p, "driver"))
		case "bind":
			link(t, native, filepath.Join(p, "driver"))
		default:
			t.Fatalf("unexpected write %s", path)
		}
		return nil
	}
	err := c.Bind(context.Background(), system.BindingRequest{PCIAddress: pf, Driver: system.NativeDriver, ExpectedDriver: system.Pointer(system.VFIODriver)})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"driver_override=rebellions", "unbind=" + pf, "bind=" + pf, "driver_override="}
	if !reflect.DeepEqual(writes, want) {
		t.Fatalf("write order: %v", writes)
	}
	writes = nil
	if err := c.Bind(context.Background(), system.BindingRequest{PCIAddress: pf, Driver: system.NativeDriver, ExpectedDriver: system.Pointer(system.VFIODriver)}); !errors.Is(err, system.ErrConflict) || len(writes) != 0 {
		t.Fatalf("stale precondition wrote: %v %v", err, writes)
	}
	c.write = func(context.Context, string, string) error {
		writes = append(writes, "attempt")
		return system.ErrPermissionDenied
	}
	if err := c.Unbind(context.Background(), system.BindingRequest{PCIAddress: pf}); !errors.Is(err, system.ErrPermissionDenied) || len(writes) != 1 {
		t.Fatalf("write failure replayed: %v %v", err, writes)
	}
}

func TestCannotBindHostingPFToVFIO(t *testing.T) {
	c := fixture(t)
	partition(t, c)
	c.write = func(context.Context, string, string) error { t.Fatal("must not write"); return nil }
	if err := c.Bind(context.Background(), system.BindingRequest{PCIAddress: pf, Driver: system.VFIODriver}); !errors.Is(err, system.ErrConflict) {
		t.Fatalf("hosting PF bind: %v", err)
	}
}

func TestUsageAndUnreadableProc(t *testing.T) {
	c := fixture(t)
	addPCI(t, c, pf, system.NativeDriver)
	addNative(t, c, pf, "rbln0", "accel", "accel/accel4", 0)
	put(t, filepath.Join(c.opts.ProcRoot, "123/comm"), "worker")
	link(t, "/dev/accel/accel4", filepath.Join(c.opts.ProcRoot, "123/fd/8"))
	usage, err := c.DeviceUsage(context.Background(), []string{pf})
	if err != nil || len(usage) != 1 || !reflect.DeepEqual(usage[0].Holders, []Process{{PID: 123, Command: "worker"}}) {
		t.Fatalf("usage: %+v %v", usage, err)
	}
	put(t, filepath.Join(c.opts.ProcRoot, "456/fd"), "not a directory")
	if _, err := c.DeviceUsage(context.Background(), []string{pf}); err == nil {
		t.Fatal("unreadable process treated as unused")
	}
}

func TestMountBoundariesAndPCIIDs(t *testing.T) {
	c := fixture(t)
	put(t, filepath.Join(c.opts.ProcRoot, "self/mountinfo"), "10 1 0:1 / /driver rw - tmpfs tmpfs rw\n11 10 0:2 / /driver/lib\\040files rw - tmpfs tmpfs rw\n12 1 0:3 / /driver-other rw - tmpfs tmpfs rw\n")
	m, err := c.RootfsMounts(context.Background(), "/driver")
	if err != nil || len(m) != 2 || m[0].Path != "/driver/lib files" {
		t.Fatalf("mounts: %+v %v", m, err)
	}
	p, err := ParsePCIIDs(strings.NewReader("1eff Rebellions\n\t1220  RBLN Test (PF)\n\t\t1220 0000 subsystem\n2222 Other\n\t1220 wrong\n"))
	if err != nil || p.Lookup(0x1220) != "RBLN Test (PF)" {
		t.Fatalf("pci.ids: %q %v", p.Lookup(0x1220), err)
	}
	if d := Distance([]string{"root", "bridge", "a"}, []string{"root", "bridge", "b"}); !d.Known || d.Hops != 2 {
		t.Fatalf("distance: %+v", d)
	}
	if d := Distance([]string{"root-a"}, []string{"root-b"}); d.Known || !d.SeparateRoots {
		t.Fatalf("roots: %+v", d)
	}
}

func TestCanceledCallsDoNotWrite(t *testing.T) {
	c := fixture(t)
	addPCI(t, c, pf, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.write = func(context.Context, string, string) error { t.Fatal("canceled write"); return nil }
	if err := c.Unbind(ctx, system.BindingRequest{PCIAddress: pf}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := c.ListDevices(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestUnbindAutoprobePolicy(t *testing.T) {
	for _, allow := range []bool{false, true} {
		c := fixture(t)
		p := addPCI(t, c, pf, system.NativeDriver)
		put(t, filepath.Join(p, "driver_override"), "rebellions")
		var writes []string
		c.write = func(_ context.Context, path, value string) error {
			writes = append(writes, filepath.Base(path)+"="+value)
			if filepath.Base(path) == "unbind" {
				return os.Remove(filepath.Join(p, "driver"))
			}
			put(t, path, value)
			return nil
		}
		if err := c.Unbind(context.Background(), system.BindingRequest{PCIAddress: pf, AllowAutoprobeAfterUnbind: allow}); err != nil {
			t.Fatal(err)
		}
		want := []string{"driver_override=none", "unbind=" + pf}
		if allow {
			want = []string{"unbind=" + pf, "driver_override="}
		}
		if !reflect.DeepEqual(writes, want) {
			t.Fatalf("autoprobe=%v writes %v want %v", allow, writes, want)
		}
	}
}

func TestDeviceNotFoundDistinguishesMissingAttribute(t *testing.T) {
	c := fixture(t)
	_, err := c.GetDevice(context.Background(), pf)
	if !errors.Is(err, system.ErrNotFound) || !errors.Is(err, system.ErrDeviceNotFound) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("absent device: %v", err)
	}
	p := addPCI(t, c, pf, "")
	if err := os.Remove(filepath.Join(p, "device")); err != nil {
		t.Fatal(err)
	}
	_, err = c.GetDevice(context.Background(), pf)
	if !errors.Is(err, system.ErrNotFound) || errors.Is(err, system.ErrDeviceNotFound) {
		t.Fatalf("missing attribute on present device: %v", err)
	}
}
