/*
Copyright 2026 Rebellions Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package device

import (
	"context"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/rbln-sw/rblnlib-go/pkg/rblnsmi"
)

var getDeviceInfo = rblnsmi.GetDeviceInfo
var getPCIERootID = resolvePCIERootID

const pciSysfsDevicesPath = "/sys/bus/pci/devices"

var pciAddressPattern = regexp.MustCompile(`^\d{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}\.[0-7]$`)

// Device is a normalized view of information for an individual NPU device.
type Device struct {
	Name             string
	ProductName      string
	SID              string
	UUID             string
	MemoryTotalBytes int64
	PCIDeviceID      string
	PCIBusID         string
	PCIERootID       string
	PCINumaNode      string
	PCILinkSpeed     string
	PCILinkWidth     string
	FirmwareVersion  string
	KMDVersion       string
}

func resolvePCIERootID(pciBusID string) (string, error) {
	devicePath := filepath.Join(pciSysfsDevicesPath, pciBusID)
	resolvedPath, err := filepath.EvalSymlinks(devicePath)
	if err != nil {
		return "", err
	}

	segments := strings.Split(filepath.Clean(resolvedPath), string(filepath.Separator))
	for _, segment := range segments {
		if pciAddressPattern.MatchString(segment) {
			return segment, nil
		}
	}
	return "", nil
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
		pciRootID, _ := getPCIERootID(device.PCI.BusID)

		devices = append(devices, Device{
			Name:             device.Device, // e.g rbln0, rbln1
			ProductName:      device.Name,   // e.g RBLN-CA25, RBLN-CR03
			SID:              device.SID,
			UUID:             device.UUID,
			MemoryTotalBytes: memTotalBytes,
			PCIDeviceID:      device.PCI.Dev,
			PCIBusID:         device.PCI.BusID,
			PCIERootID:       pciRootID,
			PCINumaNode:      device.PCI.NUMANode,
			PCILinkSpeed:     device.PCI.LinkSpeed,
			PCILinkWidth:     device.PCI.LinkWidth,
			FirmwareVersion:  device.FWVer,
			KMDVersion:       smiInfo.KMDVersion,
		})
	}
	return devices, nil
}
