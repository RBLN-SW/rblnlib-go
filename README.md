# rblnlib-go

A high-level library that provides a Go abstraction over Rebellions' NPU management library. With this library, you can query NPU information in a type-safe way, check device status, and control NPUs.

## Packages

| Package | Description |
|---------|-------------|
| rblnsmi | Provides a Go abstraction of the `rbln-smi` command. |
| rsdgroup | Provides functionality to group multiple NPUs into a single RSD Group. |
| device | Provides NPU information lookup, status checking, and hardware information. |

## Notes

To retrieve NPU device information, all packages above have a direct dependency on the `rbln-smi` binary. Therefore, in the runtime environment where this package is executed, the `rbln-smi` binary must be installed and executable.

### Installation

```bash
go get github.com/rbln-sw/rblnlib-go
```
