package domain

import (
	"testing"
	"time"
)

func validFixture() (Configuration, Inventory, Draft) {
	c := Configuration{Serving: Serving{Model: "selected", Context: 32768, Concurrency: 2, MemoryFraction: .8}, Resources: Resources{CPU: 20, MemoryMiB: 32768, SharedMemoryMiB: 16384, GPUCount: 1}}
	c.Revision = c.ContentRevision()
	inv := Inventory{Target: "test-node", Models: []Model{{ID: "selected", GPUCount: 1}}, Hardware: Hardware{ObservedAt: time.Now(), BootID: "boot-a", CurrentBootID: "boot-a", MemoryMiB: 65536, DIMMs: 2, CPUThreads: 48, SMTWidth: 2, TopologyKnown: true, CPUManagerPolicy: "static", FullPCPUsOnly: true, HostReserveMiB: 12288, KubeReserveMiB: 6144, ReservedCPU: 6, GPUs: []GPU{{ID: "pci-a", Model: "duplicate", RenderPath: "/dev/dri/renderD129"}, {ID: "pci-b", Model: "duplicate", RenderPath: "/dev/dri/renderD128"}}}}
	d := Draft{Action: "serving.configure", Target: inv.Target, SourceRevision: c.Revision}
	return c, inv, d
}
func TestHardwareBudgets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Configuration, *Inventory, *Draft)
		want   bool
	}{
		{"two DIMMs", func(*Configuration, *Inventory, *Draft) {}, true},
		{"four DIMMs rediscovered", func(c *Configuration, i *Inventory, d *Draft) { i.Hardware.DIMMs = 4; i.Hardware.MemoryMiB = 131072 }, true},
		{"unknown topology", func(c *Configuration, i *Inventory, d *Draft) { i.Hardware.TopologyKnown = false }, false},
		{"insufficient memory", func(c *Configuration, i *Inventory, d *Draft) { i.Hardware.OtherMemoryMiB = 20000 }, false},
		{"SMT split", func(c *Configuration, i *Inventory, d *Draft) { c.Resources.CPU = 19 }, false},
		{"duplicate names enumeration changes", func(c *Configuration, i *Inventory, d *Draft) {
			i.Hardware.GPUs[0], i.Hardware.GPUs[1] = i.Hardware.GPUs[1], i.Hardware.GPUs[0]
		}, true},
		{"physical placement", func(c *Configuration, i *Inventory, d *Draft) { c.Resources.PhysicalGPU = "pci-a" }, false},
		{"stale boot", func(c *Configuration, i *Inventory, d *Draft) { i.Hardware.CurrentBootID = "boot-b" }, false},
		{"expired report", func(c *Configuration, i *Inventory, d *Draft) {
			i.Hardware.ObservedAt = time.Now().Add(-25 * time.Hour)
		}, false},
		{"future report", func(c *Configuration, i *Inventory, d *Draft) { i.Hardware.ObservedAt = time.Now().Add(time.Hour) }, false},
		{"other GPU consumers", func(c *Configuration, i *Inventory, d *Draft) { i.Hardware.OtherGPUs = 2 }, false},
		{"no CPU manager", func(c *Configuration, i *Inventory, d *Draft) { i.Hardware.CPUManagerPolicy = "none" }, false},
		{"global output cap unsupported", func(c *Configuration, i *Inventory, d *Draft) { c.Serving.MaxOutputTokens = 2048 }, false},
		{"offload plus shm exceeds memory", func(c *Configuration, i *Inventory, d *Draft) { c.Serving.CPUOffloadGiB = 16 }, false},
		{"model name cannot qualify fewer GPUs", func(c *Configuration, i *Inventory, d *Draft) { i.Models[0].GPUCount = 2 }, false},
		{"unknown action", func(c *Configuration, i *Inventory, d *Draft) { d.Action = "shell" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, i, d := validFixture()
			tc.mutate(&c, &i, &d)
			err := ValidateDraft(d, c, i)
			if (err == nil) != tc.want {
				t.Fatalf("valid=%v err=%v", tc.want, err)
			}
		})
	}
}
