// Package smi implements typed SMI queries and RSD control for system/v1.
// It supports the legacy Python and current Rust JSON scalar encodings, keeps
// global NPU indexes separate from group-local indexes, and never uses a shell.
package smi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rbln-sw/rblnlib-go/internal/command"
	system "github.com/rbln-sw/rblnlib-go/pkg/system/v1"
)

type Options struct {
	// Binary selects the SMI executable. Set it only from trusted configuration.
	Binary                       string
	HostRoot                     string
	DevRoot                      string
	LockPath                     string
	QueryTimeout, CommandTimeout time.Duration
	// ReplaceIntersectingGroups retains the Kubernetes allocators' regrouping
	// policy. The caller must establish allocation ownership/quiescence first.
	// The default refuses to replace groups containing other functions.
	ReplaceIntersectingGroups bool
}
type Client struct {
	opts Options
	run  func(context.Context, *exec.Cmd) ([]byte, error)
}

func New(opts Options) (*Client, error) {
	if opts.DevRoot == "" {
		opts.DevRoot = "/dev"
	}
	// Coordinate with the existing rsdgroup package and current components.
	if opts.LockPath == "" {
		opts.LockPath = "/var/run/rbln-rsd-group.lock"
	}
	if opts.QueryTimeout == 0 {
		opts.QueryTimeout = 5 * time.Second
	}
	if opts.CommandTimeout == 0 {
		opts.CommandTimeout = 30 * time.Second
	}
	if opts.QueryTimeout < 0 || opts.CommandTimeout < 0 {
		return nil, &system.Error{Kind: system.InvalidArgument, Message: "negative SMI timeout"}
	}
	for _, path := range []string{opts.HostRoot, opts.DevRoot, opts.LockPath} {
		if path != "" && (!filepath.IsAbs(path) || filepath.Clean(path) != path) {
			return nil, &system.Error{Kind: system.InvalidArgument, Target: path}
		}
	}
	return &Client{opts: opts, run: command.Run}, nil
}

func (c *Client) execute(ctx context.Context, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	binary := c.opts.Binary
	if binary == "" {
		if c.opts.HostRoot != "" {
			binary = "rbln-smi"
		} else {
			for _, p := range []string{"/usr/bin/rbln-smi", "/run/rbln/driver/usr/bin/rbln-smi", "/host/usr/bin/rbln-smi", "/host/driver/usr/bin/rbln-smi"} {
				if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
					binary = p
					break
				}
			}
			if binary == "" {
				var err error
				binary, err = exec.LookPath("rbln-smi")
				if err != nil {
					return nil, &system.Error{Kind: system.Unsupported, Op: "find rbln-smi", Cause: err}
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.opts.HostRoot != "" {
		args = append([]string{c.opts.HostRoot, binary}, args...)
		return c.run(ctx, exec.CommandContext(ctx, "chroot", args...))
	}
	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command -- binary is the configured SMI executable or a discovered rbln-smi. Device input is passed only in args.
	return c.run(ctx, exec.CommandContext(ctx, binary, args...))
}

type scalar string

func (s *scalar) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*s = ""
		return nil
	}
	var str string
	if len(data) > 0 && data[0] == '"' {
		if err := json.Unmarshal(data, &str); err != nil {
			return err
		}
		*s = scalar(str)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return err
	}
	*s = scalar(number.String())
	return nil
}

type rawDevice struct {
	normalizedPCI string
	NPU           scalar `json:"npu"`
	Group         scalar `json:"group_id"`
	Name          string `json:"device"`
	Product       string `json:"name"`
	UUID          string `json:"uuid"`
	Serial        string `json:"sid"`
	Status        string `json:"status"`
	Firmware      string `json:"fw_ver"`
	PCI           struct {
		Address string `json:"bus_id"`
		ID      scalar `json:"dev"`
		NUMA    scalar `json:"numa_node"`
		Speed   scalar `json:"link_speed"`
		Width   scalar `json:"link_width"`
	} `json:"pci"`
	Memory struct {
		Total scalar `json:"total"`
	} `json:"memory"`
}
type rawInventory struct {
	Driver  string       `json:"KMD_version"`
	Devices *[]rawDevice `json:"devices"`
}

func (c *Client) inventory(ctx context.Context, grouped bool) (rawInventory, error) {
	args := []string{"-j"}
	if grouped {
		args = []string{"-g", "-j"}
	}
	data, err := c.execute(ctx, c.opts.QueryTimeout, args...)
	if err != nil {
		return rawInventory{}, err
	}
	var raw rawInventory
	if err := json.Unmarshal(data, &raw); err != nil {
		return raw, &system.Error{Kind: system.InvalidData, Op: "decode SMI inventory", Cause: err}
	}
	if raw.Devices == nil {
		return raw, system.FieldError("rbln-smi", "devices", "missing or null array")
	}
	seenNames, seenPCI := map[string]bool{}, map[string]bool{}
	for i := range *raw.Devices {
		d := &(*raw.Devices)[i]
		a, err := system.NormalizePCIAddress(d.PCI.Address)
		if err != nil || system.ValidTargets([]string{d.Name}) != nil {
			return raw, system.FieldError(d.Name, "SMI identity", d.PCI.Address)
		}
		if seenNames[d.Name] || seenPCI[a] {
			return raw, system.FieldError(d.Name, "SMI identity", "duplicate")
		}
		seenNames[d.Name], seenPCI[a], d.normalizedPCI = true, true, a
	}
	return raw, nil
}

func (c *Client) ListDevices(ctx context.Context) (system.Snapshot, error) {
	s := system.Snapshot{Source: "smi/json", ObservedAt: time.Now(), Devices: []system.Device{}}
	raw, err := c.inventory(ctx, false)
	if err != nil {
		return s, err
	}
	for _, entry := range *raw.Devices {
		d := system.Device{PCIAddress: entry.normalizedPCI, Name: entry.Name, UUID: entry.UUID, Serial: entry.Serial, ProductName: entry.Product, VendorID: system.VendorID, Class: system.NPUClass, State: system.StateUnknown, FunctionKind: system.FunctionUnknown, Versions: system.Versions{Driver: raw.Driver, Firmware: entry.Firmware}}
		d.ReportedPCI = &system.ReportedPCI{
			Address: entry.PCI.Address, DeviceID: string(entry.PCI.ID),
			NUMANode: system.Pointer(string(entry.PCI.NUMA)), LinkSpeed: system.Pointer(string(entry.PCI.Speed)),
			LinkWidth: system.Pointer(string(entry.PCI.Width)),
		}
		issue := func(field string, value any) {
			d.Issues = append(d.Issues, system.Issue{Target: d.Name, Field: field, Err: system.FieldError(d.Name, field, value)})
		}
		if n, e := strconv.ParseUint(string(entry.NPU), 10, 32); e == nil {
			d.Index = system.Pointer(uint32(n))
		} else if entry.NPU != "" && entry.NPU != "N/A" {
			issue("index", entry.NPU)
		}
		if n, e := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(string(entry.PCI.ID)), "0x"), 16, 16); e == nil {
			d.DeviceID = uint16(n)
			d.DeviceIDKnown = true
		} else {
			issue("device_id", entry.PCI.ID)
		}
		if n, e := strconv.ParseUint(string(entry.Memory.Total), 10, 64); e == nil {
			d.MemoryTotalBytes = &n
		} else {
			issue("memory_total_bytes", entry.Memory.Total)
		}
		if n, e := strconv.Atoi(string(entry.PCI.NUMA)); e == nil && n >= 0 {
			d.Topology.NUMANode = &n
		} else if entry.PCI.NUMA != "-1" && entry.PCI.NUMA != "N/A" && entry.PCI.NUMA != "" {
			issue("numa_node", entry.PCI.NUMA)
		}
		if n, e := strconv.ParseUint(strings.TrimPrefix(string(entry.PCI.Width), "x"), 10, 32); e == nil && n > 0 {
			d.Topology.LinkWidth = system.Pointer(uint32(n))
		}
		if fields := strings.Fields(string(entry.PCI.Speed)); len(fields) > 0 {
			n, e := strconv.ParseFloat(strings.TrimSuffix(fields[0], "GT/s"), 64)
			if e == nil && n > 0 && !math.IsNaN(n) && !math.IsInf(n, 0) {
				d.Topology.LinkSpeedGTPerSecond = &n
			} else {
				issue("link_speed", entry.PCI.Speed)
			}
		}
		// SMI collapses BUSY/PROBING/INIT into in_reset and FAULT/FINISH into
		// fault. A precise lifecycle state must come from host or SMD.
		d.Issues = append(d.Issues, system.Issue{Target: d.Name, Field: "state", Err: &system.Error{Kind: system.Unsupported, Message: "SMI exposes coarse lifecycle status: " + entry.Status}})
		s.Devices = append(s.Devices, d)
	}
	return s, nil
}

func (c *Client) Version(ctx context.Context) (string, error) {
	out, err := c.execute(ctx, c.opts.QueryTimeout, "--version")
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return "", system.FieldError("rbln-smi", "version", "empty")
	}
	return v, nil
}

// mutate never retries. Any execution failure after invocation is conservative:
// the command may already have changed hardware even with a non-zero exit.
func (c *Client) mutate(ctx context.Context, args ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := c.execute(ctx, c.opts.CommandTimeout, args...)
	if err != nil {
		return errors.Join(system.ErrIndeterminate, err)
	}
	return nil
}

var _ system.Inventory = (*Client)(nil)
