// Command system-inventory prints local hardware facts without invoking SMI,
// connecting to SMD, opening NPU nodes or changing device configuration.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	host "github.com/rbln-sw/rblnlib-go/pkg/host/v1"
)

func main() {
	sysRoot := flag.String("sys-root", "/sys", "complete host sysfs mount")
	devRoot := flag.String("dev-root", "/dev", "host device mount")
	flag.Parse()
	c, err := host.New(host.Options{SysRoot: *sysRoot, DevRoot: *devRoot})
	if err != nil {
		log.Fatal(err)
	}
	snapshot, err := c.ListDevices(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	for _, issue := range snapshot.Issues {
		log.Printf("%s %s: %v", issue.Target, issue.Field, issue.Err)
	}
	for _, d := range snapshot.Devices {
		fmt.Printf("pci=%s name=%s driver=%s state=%s kind=%s\n", d.PCIAddress, d.Name, d.Driver, d.State, d.FunctionKind)
		for _, node := range d.Nodes {
			fmt.Printf("  %s: host=%s path=%s\n", node.Kind, node.HostPath, node.Path)
		}
		for _, issue := range d.Issues {
			log.Printf("%s %s: %v", issue.Target, issue.Field, issue.Err)
		}
	}
}
