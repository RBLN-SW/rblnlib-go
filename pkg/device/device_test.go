package device

import (
	"context"
	"errors"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/rbln-sw/rblnlib-go/pkg/rblnsmi"
)

func TestDevice(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Device Suite")
}

var _ = ginkgo.Describe("GetDevices", func() {
	originalGetDeviceInfo := getDeviceInfo

	ginkgo.AfterEach(func() {
		getDeviceInfo = originalGetDeviceInfo
	})

	ginkgo.It("returns an error when device info retrieval fails", func() {
		expectedErr := errors.New("failed to get device info")
		getDeviceInfo = func(context.Context) (*rblnsmi.RblnSmi, error) {
			return nil, expectedErr
		}

		devices, err := GetDevices(context.Background())

		gomega.Expect(err).To(gomega.MatchError(expectedErr))
		gomega.Expect(devices).To(gomega.BeNil())
	})

	ginkgo.It("maps rbln-smi device info into device list", func() {
		getDeviceInfo = func(context.Context) (*rblnsmi.RblnSmi, error) {
			return &rblnsmi.RblnSmi{
				KMDVersion: "kmd-1.2.3",
				Devices: []rblnsmi.Device{
					{
						Device: "rbln0",
						Name:   "RBLN-CA25",
						SID:    "sid-0",
						UUID:   "uuid-0",
						FWVer:  "fw-0.9.0",
						Memory: rblnsmi.Memory{
							Total: "17179869184",
						},
						PCI: rblnsmi.PCIInfo{
							Dev:       "0x1234",
							BusID:     "0000:01:00.0",
							NUMANode:  "0",
							LinkSpeed: "16GT/s",
							LinkWidth: "x16",
						},
					},
				},
			}, nil
		}

		devices, err := GetDevices(context.Background())

		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(devices).To(gomega.HaveLen(1))
		gomega.Expect(devices[0]).To(gomega.Equal(Device{
			Name:             "rbln0",
			ProductName:      "RBLN-CA25",
			SID:              "sid-0",
			UUID:             "uuid-0",
			MemoryTotalBytes: 17179869184,
			PCIDeviceID:      "0x1234",
			PCIBusID:         "0000:01:00.0",
			PCINumaNode:      "0",
			PCILinkSpeed:     "16GT/s",
			PCILinkWidth:     "x16",
			FirmwareVersion:  "fw-0.9.0",
			KMDVersion:       "kmd-1.2.3",
		}))
	})
})
