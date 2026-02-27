package device

import (
	"context"
	"strconv"

	"github.com/rbln-sw/rblnlib-go/pkg/rblnsmi"
)

var getDeviceInfo = rblnsmi.GetDeviceInfo

// Device is a normalized view of information for an individual NPU device.
type Device struct {
	Name             string
	ProductName      string
	SID              string
	UUID             string
	MemoryTotalBytes int64
	PCIDeviceID      string
	PCIBusID         string
	PCINumaNode      string
	PCILinkSpeed     string
	PCILinkWidth     string
	FirmwareVersion  string
	KMDVersion       string
}

// GetDevices returns information for all devices available on the current host.
func GetDevices(ctx context.Context) ([]Device, error) {
	smiInfo, err := getDeviceInfo(ctx)
	if err != nil {
		return nil, err
	}
	devices := make([]Device, 0, len(smiInfo.Devices))
	for _, device := range smiInfo.Devices {
		memTotalBytes, _ := strconv.ParseInt(device.Memory.Total, 10, 64)

		devices = append(devices, Device{
			Name:             device.Device, // e.g rbln0, rbln1
			ProductName:      device.Name,   // e.g RBLN-CA25, RBLN-CR03
			SID:              device.SID,
			UUID:             device.UUID,
			MemoryTotalBytes: memTotalBytes,
			PCIDeviceID:      device.PCI.Dev,
			PCIBusID:         device.PCI.BusID,
			PCINumaNode:      device.PCI.NUMANode,
			PCILinkSpeed:     device.PCI.LinkSpeed,
			PCILinkWidth:     device.PCI.LinkWidth,
			FirmwareVersion:  device.FWVer,
			KMDVersion:       smiInfo.KMDVersion,
		})
	}
	return devices, nil
}
