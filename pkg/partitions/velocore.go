package partitions

import (
	"fmt"
	"strings"
)

// Partition represents a known eMMC partition.
type Partition struct {
	Name     string
	StartLBA uint64
	SizeLBA  uint64
}

// SizeBytes returns the partition size in bytes.
func (p *Partition) SizeBytes() uint64 {
	return p.SizeLBA * 512
}

// SizeHuman returns a human-readable size string.
func (p *Partition) SizeHuman() string {
	bytes := p.SizeBytes()
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// VeloCore is the embedded partition table for the Bowflex VeloCore (RK3399).
// 28 partitions extracted from the device's GPT + chunked_dump.sh.
var VeloCore = []Partition{
	{"uboot_a", 0x4000, 0x2000},
	{"uboot_b", 0x6000, 0x2000},
	{"trust_a", 0x8000, 0x2000},
	{"trust_b", 0xa000, 0x2000},
	{"misc", 0xc000, 0x2000},
	{"resource", 0xe000, 0x8000},
	{"kernel", 0x16000, 0x10000},
	{"dtb", 0x26000, 0x2000},
	{"dtbo_a", 0x28000, 0x2000},
	{"dtbo_b", 0x2a000, 0x2000},
	{"vbmeta_a", 0x2c000, 0x800},
	{"vbmeta_b", 0x2c800, 0x800},
	{"boot_a", 0x2d000, 0x20000},
	{"boot_b", 0x4d000, 0x20000},
	{"backup", 0x6d000, 0x38000},
	{"security", 0xa5000, 0x2000},
	{"cache", 0xa7000, 0x100000},
	{"system_a", 0x1a7000, 0x500000},
	{"system_b", 0x6a7000, 0x500000},
	{"metadata", 0xba7000, 0x8000},
	{"vendor_a", 0xbaf000, 0x100000},
	{"vendor_b", 0xcaf000, 0x100000},
	{"oem_a", 0xdaf000, 0x100000},
	{"oem_b", 0xeaf000, 0x100000},
	{"frp", 0xfaf000, 0x400},
	{"sw_release", 0xfaf400, 0xed7000},
	{"video", 0x1e86400, 0x47e000},
	{"userdata", 0x2304400, 0x1739bdf},
}

// Lookup finds a partition by name (case-insensitive).
func Lookup(name string) (*Partition, error) {
	lower := strings.ToLower(name)
	for i := range VeloCore {
		if strings.ToLower(VeloCore[i].Name) == lower {
			return &VeloCore[i], nil
		}
	}
	return nil, fmt.Errorf("unknown partition: %q (use 'velotool partitions' to list)", name)
}
