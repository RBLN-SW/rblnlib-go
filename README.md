# rblnlib-go

`rblnlib-go` is a Go library for working with Rebellions NPUs from Go. Its versioned system interfaces isolate applications from sysfs layouts, device-node naming, SMI JSON and SMD protocol changes.

This library is intended for infrastructure components such as operators, device plugins, schedulers, and other automation that need a stable way to interact with RBLN devices.

## What It Provides

- `pkg/system/v1`: shared device, topology, state, telemetry, error and command contracts
- `pkg/host/v1`: local PCI/sysfs discovery, legacy/accel nodes, PF/VF and IOMMU topology, binding, process usage, modules and driver rootfs operations
- `pkg/smi/v1`: normalized SMI inventory and verified RSD group recreation
- `pkg/smd/v1`: SMD v1/legacy queries, normalized telemetry and explicit SR-IOV control
- `pkg/device`: normalized device inventory for the current host
- `pkg/rblnsmi`: thin wrapper around the `rbln-smi` binary
- `pkg/rsdgroup`: high-level helper for recreating RSD groups safely

The original three packages retain their v0.3.0 APIs. For new integrations, see the [v1 system API contract](docs/system-interface-v1.md).

## Requirements

- Go `1.25.4` or newer
- Linux for production host access; filesystem and RPC fixtures also run on macOS
- A complete host sysfs/devfs view for `host`, an executable `rbln-smi` for `smi` and the legacy packages, or an SMD endpoint for `smd`

The host and SMD packages do not require SMI, CGO or `librbln-ml.so`. Mutating host operations need the corresponding host permissions; SMD commands need the daemon's configured authentication and authorization.

## Versioned system API

```go
local, err := host.New(host.Options{}) // import github.com/rbln-sw/rblnlib-go/pkg/host/v1
if err != nil {
    return err
}
snapshot, err := local.ListDevices(ctx)
if err != nil {
    return err
}
for _, device := range snapshot.Devices {
    // Inspect device.Issues before using an unavailable field.
    // Pass the returned paths through; never build /dev paths from Name.
    for _, node := range device.Nodes {
        fmt.Println(node.HostPath, node.Path)
    }
}
// A consumer that needs a complete view can additionally check snapshot.Err().
```

Run the read-only example with `go run ./examples/system-inventory`. For SMD,
construct `smd.New(smd.Options{Endpoint: address, TLS: tlsConfig})`; nil TLS
selects plaintext. `ListDevices` reads inventory and `SampleTelemetry` reads a
pre-provisioned clock-free group. `EnsureTelemetryGroup` is a separate explicit
configuration operation. `EnableVFs`/`DisableVFs` return per-target outcomes and
never retry a command automatically.

The `v1` contract preserves field meanings, units, missing-value rules and error
categories. A different actual device path under a newer driver is compatible;
changing what a field means requires a new API. Consumers still need a library
dependency update and rebuilt image to receive compatibility fixes.

## Installation

```bash
go get github.com/rbln-sw/rblnlib-go
```

## Quick Start

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/rbln-sw/rblnlib-go/pkg/device"
)

func main() {
	devices, err := device.GetDevices(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	for _, d := range devices {
		fmt.Printf(
			"%s (%s) pci=%s firmware=%s memory=%d\n",
			d.Name,
			d.ProductName,
			d.PCIBusID,
			d.FirmwareVersion,
			d.MemoryTotalBytes,
		)
	}
}
```

## Package Overview

### `pkg/device`

Use `device.GetDevices(ctx)` when you need a normalized device view for scheduling, inventory, or reporting. It returns PCI metadata, firmware version, memory size, UUID, SID, and KMD version in Go structs.

### `pkg/rblnsmi`

Use `rblnsmi.GetDeviceInfo(ctx)` when you want access to the raw `rbln-smi` model, or `rblnsmi.CreateRsdGroup` and `rblnsmi.DestroyRsdGroup` when you need explicit control over RSD group lifecycle.

### `pkg/rsdgroup`

Use `rsdgroup.RecreateRsdGroup(deviceIDs)` when you want a higher-level helper that destroys existing groups for the target devices, recreates the group, and returns the corresponding `/dev/rsd*` path. It now returns an error instead of silently falling back to `/dev/rsd0`.

## Development

```bash
make test
make test-race
make vet
make check-linux
make fmt
make lint
```
