# SMD wire definitions

These schemas are internal inputs to the SMD adapter. Public callers use
`pkg/system/v1` types and `pkg/smd/v1` methods, not generated protobuf types.

| Schema | Source |
| --- | --- |
| `rbln_smd_v1.proto` | `ssw-common-tools/rbln-smd/proto/public/rbln_smd_v1.proto`, commit `e7b9c0f835dc318b181641b36727df32f30204fc` |
| `rbln_services.proto` | Legacy schema retained by `rbln-metrics-exporter/api/rbln_services.proto`, commit `625d7beaac210709a430dddd9f3594073fb2b9d6` |

Only the `go_package` option is adjusted. Preserve the upstream license and
wire field numbers. Update this provenance when syncing SSW. Generated code is
checked in under `internal/proto`; consumers must not copy it again.

Regenerate from the repository root using `make generate-proto`. It requires
`protoc` on PATH and installs pinned `protoc-gen-go` v1.36.11 and
`protoc-gen-go-grpc` v1.5.1 into `bin/proto-tools`. Review generated changes and
run the SMD contract tests before updating the supported compatibility matrix.

An SSW wire-schema revision does not itself change the public Go API version.
Adapters must preserve the semantic contract or add a new versioned API.
