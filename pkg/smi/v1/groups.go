package smi

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/rbln-sw/rblnlib-go/internal/hostlock"
	system "github.com/rbln-sw/rblnlib-go/pkg/system/v1"
)

type Group struct {
	ID           uint32
	PCIAddresses []string
	Node         system.DeviceNode
}

// ValidateGroupMembers verifies the exact requested set in one private group.
// Ordering is irrelevant for this membership check; callers retain group-local
// ordering separately. Group zero can never satisfy a private allocation.
func ValidateGroupMembers(groupID uint32, addresses []string, groups []Group) error {
	if groupID == 0 {
		return &system.Error{Kind: system.InvalidArgument, Message: "default RSD group is not private"}
	}
	if err := system.ValidTargets(addresses); err != nil {
		return err
	}
	wanted := map[string]bool{}
	for _, a := range addresses {
		n, err := system.NormalizePCIAddress(a)
		if err != nil {
			return err
		}
		if wanted[n] {
			return system.FieldError(a, "RSD members", "duplicate target")
		}
		wanted[n] = true
	}
	found := false
	for _, g := range groups {
		if g.ID != groupID {
			continue
		}
		if found {
			return system.FieldError("RSD group", "ID", "duplicate group")
		}
		found = true
		seen := map[string]bool{}
		for _, a := range g.PCIAddresses {
			n, err := system.NormalizePCIAddress(a)
			if err != nil {
				return err
			}
			if !wanted[n] || seen[n] {
				return system.FieldError(a, "RSD membership", "unexpected or duplicate device")
			}
			seen[n] = true
		}
		if len(seen) != len(wanted) {
			return system.FieldError("RSD group", "membership", "missing requested devices")
		}
	}
	if !found {
		return &system.Error{Kind: system.NotFound, Message: "requested RSD group missing"}
	}
	return nil
}

func (c *Client) ListGroups(ctx context.Context) ([]Group, error) {
	raw, err := c.inventory(ctx, true)
	if err != nil {
		return nil, err
	}
	groups := map[uint32][]string{}
	for _, d := range *raw.Devices {
		n, err := strconv.ParseUint(string(d.Group), 10, 32)
		if err != nil {
			return nil, system.FieldError(d.Name, "group_id", d.Group)
		}
		groups[uint32(n)] = append(groups[uint32(n)], d.normalizedPCI)
	}
	out := make([]Group, 0, len(groups))
	for id, members := range groups {
		// Preserve the backend's group-local ordering, not PCI order.
		name := "rsd" + strconv.FormatUint(uint64(id), 10)
		out = append(out, Group{ID: id, PCIAddresses: members, Node: system.DeviceNode{Path: filepath.Join("/dev", name), HostPath: filepath.Join(c.opts.DevRoot, name), Kind: "rsd"}})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// RecreateGroup reuses a private group with exactly the requested members.
// Otherwise it regroups the devices and verifies membership after creation.
// Replacing a group with unrequested members requires ReplaceIntersectingGroups.
// Returned node paths are not proof of udev readiness; the caller may use
// host.WaitForNode before injecting them. Group 0 is never destroyed.
func (c *Client) RecreateGroup(ctx context.Context, addresses []string) (group Group, err error) {
	mutated := false
	defer func() {
		if mutated && err != nil {
			err = errors.Join(system.ErrIndeterminate, err)
		}
	}()
	if err := system.ValidTargets(addresses); err != nil {
		return group, err
	}
	wanted := map[string]bool{}
	for _, address := range addresses {
		a, err := system.NormalizePCIAddress(address)
		if err != nil {
			return group, err
		}
		if wanted[a] {
			return group, &system.Error{Kind: system.InvalidArgument, Target: a, Message: "duplicate PCI target"}
		}
		wanted[a] = true
	}
	ctx, cancel := context.WithTimeout(ctx, c.opts.CommandTimeout)
	defer cancel()
	release, err := hostlock.Acquire(ctx, c.opts.LockPath)
	if err != nil {
		return group, err
	}
	defer func() { err = errors.Join(err, release()) }()
	// Probe the command contract: released builds reuse version strings while
	// changing numeric selector semantics. PCI selectors avoid those ordinals.
	help, err := c.execute(ctx, c.opts.QueryTimeout, "group", "--help")
	if err != nil {
		return group, err
	}
	contract := strings.ToLower(strings.Join(strings.Fields(string(help)), " "))
	pciSelectors := strings.Contains(contract, "pci address")
	if !pciSelectors && !strings.Contains(contract, "attach npu ids") {
		return group, &system.Error{Kind: system.Unsupported, Message: "unknown SMI group selector contract"}
	}
	groups, err := c.ListGroups(ctx)
	if err != nil {
		return group, err
	}
	present := map[string]bool{}
	for _, g := range groups {
		for _, address := range g.PCIAddresses {
			present[address] = true
		}
	}
	for address := range wanted {
		if !present[address] {
			return group, &system.Error{Kind: system.NotReady, Target: address, Message: "requested function absent from current inventory"}
		}
	}
	remove := []string{}
	used := map[uint32]bool{0: true}
	for _, g := range groups {
		used[g.ID] = true
		if g.ID == 0 {
			continue
		}
		overlaps, outside := false, false
		for _, a := range g.PCIAddresses {
			if wanted[a] {
				overlaps = true
			} else {
				outside = true
			}
		}
		// Inventory contains one entry per normalized PCI address. Preserve the
		// device node already returned to earlier allocations of this set.
		if !outside && len(g.PCIAddresses) == len(wanted) {
			return g, nil
		}
		if overlaps && outside && !c.opts.ReplaceIntersectingGroups {
			return group, &system.Error{Kind: system.Conflict, Message: "existing RSD group includes devices outside the request"}
		}
		if overlaps {
			remove = append(remove, strconv.FormatUint(uint64(g.ID), 10))
		}
	}
	if len(remove) > 0 {
		mutated = true
		if err := c.mutate(ctx, "group", "-d", strings.Join(remove, ",")); err != nil {
			return group, err
		}
	}
	// Removing a group renumbers the default group. Resolve only afterwards,
	// while retaining unrelated allocations and checking every target is free.
	indexes, err := c.groupSelectors(ctx, wanted, pciSelectors)
	if err != nil {
		return group, err
	}
	id := uint32(1)
	for used[id] {
		id++
		if id == 0 {
			return group, &system.Error{Kind: system.Unavailable, Message: "RSD group IDs exhausted"}
		}
	}
	mutated = true
	if err := c.mutate(ctx, "group", "-c", strconv.FormatUint(uint64(id), 10), "-a", strings.Join(indexes, ",")); err != nil {
		return group, err
	}
	groups, err = c.ListGroups(ctx)
	if err != nil {
		return group, errors.Join(system.ErrIndeterminate, err)
	}
	if err := ValidateGroupMembers(id, addresses, groups); err != nil {
		return group, errors.Join(system.ErrIndeterminate, err)
	}
	for _, g := range groups {
		if g.ID == id {
			return g, nil
		}
	}

	return group, &system.Error{Kind: system.Indeterminate, Message: "created RSD group not found in readback"}
}

func (c *Client) groupSelectors(ctx context.Context, wanted map[string]bool, pciSelectors bool) ([]string, error) {
	raw, err := c.inventory(ctx, true)
	if err != nil {
		return nil, err
	}
	indexes := []uint32{}
	addresses := []string{}
	for _, d := range *raw.Devices {
		if !wanted[d.normalizedPCI] {
			continue
		}
		if d.Group != "0" {
			return nil, &system.Error{Kind: system.NotReady, Target: d.normalizedPCI, Message: "requested function is not in the default RSD group"}
		}
		addresses = append(addresses, d.normalizedPCI)
		if pciSelectors {
			continue
		}
		n, err := strconv.ParseUint(string(d.NPU), 10, 32)
		if err != nil || slices.Contains(indexes, uint32(n)) {
			return nil, system.FieldError(d.Name, "default-group NPU index", d.NPU)
		}
		indexes = append(indexes, uint32(n))
	}
	if len(addresses) != len(wanted) {
		return nil, &system.Error{Kind: system.NotReady, Message: "requested functions missing after RSD regrouping"}
	}
	if pciSelectors {
		slices.Sort(addresses)
		return addresses, nil
	}
	slices.Sort(indexes)
	out := make([]string, len(indexes))
	for i, index := range indexes {
		out[i] = strconv.FormatUint(uint64(index), 10)
	}
	return out, nil
}
