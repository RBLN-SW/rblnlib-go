package host

import (
	"context"
	"path/filepath"
	"testing"

	system "github.com/rbln-sw/rblnlib-go/pkg/system/v1"
)

func TestNamedStateAndClassOnlyMetadata(t *testing.T) {
	c := fixture(t)
	base := filepath.Join(c.opts.SysRoot, "class/rebellions/rbln7")
	put(t, filepath.Join(base, "card_name"), " RBLN-Test \n")
	put(t, filepath.Join(base, "kernel_version"), " 4.0.0 \n")
	put(t, filepath.Join(base, "status"), "9")
	if name, err := c.ReadCardName(context.Background()); err != nil || name != "RBLN-Test" {
		t.Fatalf("product %q %v", name, err)
	}
	if version, err := c.ReadDriverVersion(context.Background()); err != nil || version != "4.0.0" {
		t.Fatalf("version %q %v", version, err)
	}
	s, err := c.ReadNamedState(context.Background(), "rbln7")
	if err != nil || s.State != system.StateUnknown || s.String() != "UNKNOWN(9)" {
		t.Fatalf("unknown observation %+v %v", s, err)
	}
	addPCI(t, c, pf, system.NativeDriver)
	addNative(t, c, pf, "rbln7", "accel", "accel/accel19", 4)
	s, err = c.ReadNamedState(context.Background(), "rbln7")
	if err != nil || s.State != system.StateFault {
		t.Fatalf("accel must take precedence: %+v %v", s, err)
	}
}

func TestBuiltinProductMetadata(t *testing.T) {
	products, err := BuiltinPCIIDs()
	if err != nil {
		t.Fatal(err)
	}
	if products.LookupHex("0X2131") != "RBLN-CR13 (VF)" || products.Lookup(0xffff) != "" {
		t.Fatal("product metadata changed")
	}
}
