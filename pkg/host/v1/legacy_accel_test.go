package host

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	system "github.com/rbln-sw/rblnlib-go/pkg/system/v1"
)

func TestGenericAccelMinorDoesNotReplaceLegacyInterface(t *testing.T) {
	c := fixture(t)
	pci := addPCI(t, c, pf, system.NativeDriver)
	addNative(t, c, pf, "rbln0", "rebellions", "rbln0", 0)
	generic := filepath.Join(c.opts.SysRoot, "class/accel/accel0")
	put(t, filepath.Join(generic, "dev"), "261:0")
	put(t, filepath.Join(generic, "uevent"), "DEVNAME=accel/accel0\nDEVTYPE=accel_minor\n")
	link(t, pci, filepath.Join(generic, "device"))
	smc := filepath.Join(c.opts.SysRoot, "class/rebellions/smc0")
	link(t, filepath.Join(c.opts.SysRoot, "class/rebellions/rbln0"), filepath.Join(smc, "device"))
	nodes, err := c.DeviceNodes(context.Background(), pf)
	if err != nil || len(nodes) != 1 || nodes[0].Path != "/dev/rbln0" {
		t.Fatalf("generic accel minor hid working legacy node: %+v %v", nodes, err)
	}
}

func TestMixedAccelClassKeepsNativeDevice(t *testing.T) {
	c := fixture(t)
	addPCI(t, c, pf, system.NativeDriver)
	addNative(t, c, pf, "rbln7", "rebellions", "rbln7", 0)
	platform := filepath.Join(c.opts.SysRoot, "devices/platform/other-accelerator")
	put(t, filepath.Join(platform, "uevent"), "DRIVER=other")
	link(t, platform, filepath.Join(c.classRoot("accel"), "accel0", "device"))
	nodes, err := c.DeviceNodes(context.Background(), pf)
	if err != nil || len(nodes) != 1 || nodes[0].Path != "/dev/rbln7" {
		t.Fatalf("unrelated platform device affected native resolution: %v, %v", nodes, err)
	}
	// A class entry claiming this PCI function must still be verified.
	put(t, filepath.Join(c.classRoot("accel"), "accel0", "pci_addr"), pf)
	if _, err := c.DeviceNodes(context.Background(), pf); !errors.Is(err, system.ErrInvalidData) {
		t.Fatalf("accepted conflicting ownership: %v", err)
	}
}
