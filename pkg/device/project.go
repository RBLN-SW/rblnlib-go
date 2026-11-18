package device

import (
	"fmt"
	"math"
	"strconv"

	system "github.com/rbln-sw/rblnlib-go/pkg/system/v1"
)

// FromSystem projects a v1 observation into the original application metadata
// shape. It performs no I/O. New system queries remain in the versioned API;
// existing resource/attribute code can migrate independently of that boundary.
func FromSystem(d system.Device) Device {
	out := Device{Name: d.Name, ProductName: d.ProductName, SID: d.Serial, UUID: d.UUID, PCIBusID: d.PCIAddress, PCIDeviceID: fmt.Sprintf("%04x", d.DeviceID), PCIERootID: d.Topology.PCIeRoot, PCINumaNode: "N/A", FirmwareVersion: d.Versions.Firmware, KMDVersion: d.Versions.Driver}
	if d.MemoryTotalBytes != nil && *d.MemoryTotalBytes <= math.MaxInt64 {
		out.MemoryTotalBytes = int64(*d.MemoryTotalBytes)
	}
	if d.Topology.NUMANode != nil {
		out.PCINumaNode = strconv.Itoa(*d.Topology.NUMANode)
	}
	if d.Topology.LinkWidth != nil {
		out.PCILinkWidth = strconv.FormatUint(uint64(*d.Topology.LinkWidth), 10)
	}
	if d.Topology.LinkSpeedGTPerSecond != nil {
		out.PCILinkSpeed = strconv.FormatFloat(*d.Topology.LinkSpeedGTPerSecond, 'f', 1, 64) + "GT/s"
	}
	if pci := d.ReportedPCI; pci != nil {
		out.PCIBusID, out.PCIDeviceID = pci.Address, pci.DeviceID
		if pci.NUMANode != nil {
			out.PCINumaNode = *pci.NUMANode
		}
		if pci.LinkSpeed != nil {
			out.PCILinkSpeed = *pci.LinkSpeed
		}
		if pci.LinkWidth != nil {
			out.PCILinkWidth = *pci.LinkWidth
		}
	}
	return out
}
