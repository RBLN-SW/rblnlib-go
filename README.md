# rblnlib-go

`rblnlib-go` is a Go library for working with Rebellions NPUs from Go. It provides a typed interface over `rbln-smi` so applications can discover devices, inspect hardware metadata, and manage RSD groups without parsing CLI output directly.

This library is intended for infrastructure components such as operators, device plugins, schedulers, and other automation that need a stable way to interact with RBLN devices.

## What It Provides

- `pkg/device`: normalized device inventory for the current host
- `pkg/rblnsmi`: thin wrapper around the `rbln-smi` binary
- `pkg/rsdgroup`: high-level helper for recreating RSD groups safely

## Requirements

- Go `1.25.4` or newer
- `rbln-smi` installed and executable in the runtime environment

`rblnlib-go` depends on `rbln-smi` at runtime. If the binary is not available, device queries and RSD group operations will fail.

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
make fmt
make lint
```
