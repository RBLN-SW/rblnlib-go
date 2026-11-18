# System API v1

The shared types and interfaces live in `pkg/system/v1`. Choose a backend
according to the observation or operation you need.

| Backend | Use |
| --- | --- |
| `pkg/host/v1` | Read live PCI identity, binding, topology and device nodes on the local host. Manage driver binding, kernel modules and RDS devices. |
| `pkg/smi/v1` | Read SMI inventory and manage RSD groups. |
| `pkg/smd/v1` | Read daemon inventory and telemetry or request VF provisioning over gRPC. |

## Identity and device nodes

A PCI address identifies a function on one host at observation time. Refresh
its identity after hotplug, reset or repartitioning. `Device.Index` is the
backend's enumeration index. `Device.NativeIndex` is the driver's PF ordinal.
Either can differ from the number in a device-node name.

Use the paths returned by the node resolver. `DeviceNode.Path` is the canonical
path under the host's `/dev`, and `HostPath` accounts for the configured mount
location. For native devices, the resolver verifies the character device's
major and minor numbers against sysfs. It supports both the legacy and accel
classes, including hosts where their device numbers differ.

## Inventory and partial results

Use `host.Client.ListDevices` to observe native, VFIO-bound and unbound NPU
functions. SMD inventory reflects the daemon's cached view. Before a local
binding operation, read the current host state. Use `host.Client.Enrich` only
when the metadata came from the same host. For matching native functions it
preserves reported PCI strings and the diagnostics for metadata fields it
uses. The host supplies the normalized topology, binding and lifecycle state.

Check the error returned by the query first. A successful query may still
carry entry failures in `Snapshot.Issues` and field failures in
`Device.Issues`. Use `Snapshot.Err()` when the operation requires a complete
view. A nil numeric pointer means the value is unavailable. A pointer to zero
represents an observed zero. An unrecognized lifecycle state is
`system.StateUnknown`.

## Commands and errors

Pass explicit targets to mutating commands. Inspect per-target outcomes from
`EnableVFs` and `DisableVFs`. If a command returns
`system.ErrIndeterminate`, read the current state before deciding what to do
next. A nonempty idempotency key identifies one logical request. Host locks
coordinate cooperating SDK callers. Other writers, including SMD, require
coordination by the caller.

Use `errors.Is` with the SDK error sentinels. A missing host PCI function
matches both `system.ErrDeviceNotFound` and `system.ErrNotFound`. A missing
attribute on a present function can also match `system.ErrNotFound`. A
transient sysfs `EAGAIN` during rebinding matches `system.ErrNotReady`.
Contexts bound RPCs, lock waits and polling. A synchronous kernel write or
unmount can continue past a context deadline.

`smi.Client.RecreateGroup` selects the RSD command syntax supported by the
installed SMI and verifies the resulting membership. By default it returns a
conflict if an intersecting group contains functions outside the request.
`smi.Options.ReplaceIntersectingGroups` enables regrouping when the caller
has established allocation ownership.

## Telemetry

Use `SampleTelemetry` for a read-only query. On SMD v1 it samples a dedicated
clock-free metric group. `EnsureTelemetryGroup` provisions that group, and
`CollectTelemetry` can provision it when `TelemetryOptions.CreateGroup` is
enabled. Legacy SMD queries use the v0 telemetry interface.

Temperature is in degrees Celsius, power in watts, utilization in percent and
memory in bytes. Byte counts use integers to preserve precision above 2^53.
PCIe link speed is in GT/s and link width is in lanes.

Board temperature and power apply to PFs, memory use and utilization to
compute functions, and performance state to PFs and VFs. If a successful v1
response omits an in-scope reading, the metric is absent from the values map
and its field issue matches `system.ErrNotReported`. An unreachable device
has a device-level issue.

## Compatibility

The v1 package paths identify the API contract independently of the Go module
release number. Existing interfaces keep their method sets so that consumer
implementations continue to compile. Add new operations through concrete
methods or separate interfaces. Changes to field meanings, units, identity
rules or error semantics require a new contract version.
