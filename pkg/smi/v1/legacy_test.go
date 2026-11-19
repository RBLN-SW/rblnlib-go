package smi

import (
	"context"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func legacyPayload(ga, gb, na, nb int) []byte {
	return []byte(fmt.Sprintf(`{"KMD_version":"3.4.1~rc1","devices":[{"device":"rbln0","npu":%d,"group_id":"%d","pci":{"bus_id":%q}},{"device":"rbln1","npu":%d,"group_id":"%d","pci":{"bus_id":%q}}]}`, na, ga, a, nb, gb, b))
}
func TestLegacyRegroupResolvesDefaultGroupAfterRemoval(t *testing.T) {
	c := smiFixture(t)
	created, removed := false, false
	var commands [][]string
	c.run = func(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
		args := cmd.Args[1:]
		switch strings.Join(args, " ") {
		case "group --help":
			return []byte("Attach NPU ids to new RSD group (use with -c)"), nil
		case "-g -j":
			if created {
				return legacyPayload(1, 1, 0, 1), nil
			}
			if removed {
				return legacyPayload(0, 0, 0, 1), nil
			}
			return legacyPayload(3, 4, 0, 0), nil
		default:
			commands = append(commands, append([]string(nil), args...))
			switch args[1] {
			case "-d":
				removed = true
			case "-c":
				created = true
			}
			return nil, nil
		}
	}
	g, err := c.RecreateGroup(context.Background(), []string{a, b})
	want := [][]string{{"group", "-d", "3,4"}, {"group", "-c", "1", "-a", "0,1"}}
	if err != nil || g.ID != 1 || !reflect.DeepEqual(commands, want) {
		t.Fatalf("%+v %v commands=%v", g, err, commands)
	}
}
func TestLegacyAllocationPreservesUnrelatedGroupAndUsesRenumberedIndex(t *testing.T) {
	c := smiFixture(t)
	created := false
	var commands [][]string
	c.run = func(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
		args := cmd.Args[1:]
		switch strings.Join(args, " ") {
		case "group --help":
			return []byte("Attach NPU ids to new RSD group"), nil
		case "-g -j":
			if created {
				return legacyPayload(1, 2, 0, 0), nil
			}
			return legacyPayload(1, 0, 0, 0), nil
		default:
			commands = append(commands, append([]string(nil), args...))
			created = true
			return nil, nil
		}
	}
	g, err := c.RecreateGroup(context.Background(), []string{b})
	if err != nil || g.ID != 2 || !reflect.DeepEqual(commands, [][]string{{"group", "-c", "2", "-a", "0"}}) {
		t.Fatalf("retained allocation: %+v %v commands=%v", g, err, commands)
	}
}
func TestExplicitRegroupPolicyRetainsExactNewMembership(t *testing.T) {
	c := smiFixture(t)
	c.opts.ReplaceIntersectingGroups = true
	created, removed := false, false
	c.run = func(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
		args := cmd.Args[1:]
		switch strings.Join(args, " ") {
		case "group --help":
			return []byte("Attach NPU ids to new RSD group"), nil
		case "-g -j":
			if created {
				return legacyPayload(1, 0, 0, 0), nil
			}
			if removed {
				return legacyPayload(0, 0, 0, 1), nil
			}
			return legacyPayload(3, 3, 0, 1), nil
		default:
			switch args[1] {
			case "-d":
				removed = true
			case "-c":
				created = true
			}
			return nil, nil
		}
	}
	g, err := c.RecreateGroup(context.Background(), []string{a})
	if err != nil || !reflect.DeepEqual(g.PCIAddresses, []string{a}) {
		t.Fatalf("%+v %v", g, err)
	}
}
func TestUnknownSelectorContractRefusesMutation(t *testing.T) {
	c := smiFixture(t)
	calls := 0
	c.run = func(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
		args := cmd.Args[1:]
		calls++
		if strings.Join(args, " ") != "group --help" {
			t.Fatal(args)
		}
		return []byte("unknown interface"), nil
	}
	if _, err := c.RecreateGroup(context.Background(), []string{a}); err == nil || calls != 1 {
		t.Fatalf("unknown contract: %v calls=%d", err, calls)
	}
}
