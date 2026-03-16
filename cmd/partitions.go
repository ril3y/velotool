package cmd

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"

	"github.com/ril3y/velotool/pkg/partitions"
	"github.com/spf13/cobra"
)

var noScan bool

func init() {
	partitionsCmd.Flags().BoolVar(&noScan, "no-scan", false, "Skip live device scan (just show partition table)")
	rootCmd.AddCommand(partitionsCmd)
}

var partitionsCmd = &cobra.Command{
	Use:   "partitions",
	Short: "List the VeloCore eMMC partition table",
	Long: `Display the embedded VeloCore partition table with LBA offsets and sizes.

If a device is connected, also reads the first sectors of each partition
to identify filesystem types and detect encryption. Use --no-scan to skip
the live device scan.`,
	Example: `  velotool partitions
  velotool partitions --no-scan`,
	Aliases: []string{"parts", "pt"},
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println()
		fmt.Printf("  %s\n", boldCyan("VeloCore RK3399 — eMMC Partition Table"))
		fmt.Printf("  %s\n", dim("29.1 GB DA4032 (HS200)"))
		fmt.Println()

		// Try to open device for live scan.
		type scanResult struct {
			format  string
			entropy float64
			status  string
		}
		results := map[string]*scanResult{}

		if !noScan {
			dev, err := openDevice()
			if err == nil {
				defer dev.Close()
				fmt.Println()
				for _, p := range partitions.VeloCore {
					data, err := dev.ReadLBA(uint32(p.StartLBA), 4)
					if err != nil {
						results[p.Name] = &scanResult{"read error", 0, red("ERROR")}
						continue
					}
					format := identifyFormat(data)
					entropy := shannonEntropy(data)
					status := assessStatus(format, entropy)
					results[p.Name] = &scanResult{format, entropy, status}
				}
			}
		}

		hasResults := len(results) > 0

		// Print header.
		if hasResults {
			fmt.Printf("  %-16s  %-12s  %-6s  %-22s  %-7s  %s\n",
				bold("NAME"), bold("START LBA"), bold("SIZE"),
				bold("FORMAT"), bold("ENTROPY"), bold("STATUS"))
		} else {
			fmt.Printf("  %-16s  %-12s  %s\n",
				bold("NAME"), bold("START LBA"), bold("SIZE"))
		}
		fmt.Printf("  %s\n", dim(strings.Repeat("─", func() int {
			if hasResults {
				return 82
			}
			return 42
		}())))

		slotA := cyan("▪")
		slotB := green("▪")
		noSlot := dim("▪")

		for _, p := range partitions.VeloCore {
			marker := noSlot
			padName := fmt.Sprintf("%-14s", p.Name)
			if strings.HasSuffix(p.Name, "_a") {
				marker = slotA
				padName = cyan(padName)
			} else if strings.HasSuffix(p.Name, "_b") {
				marker = slotB
				padName = green(padName)
			}

			if hasResults {
				r := results[p.Name]
				padFmt := fmt.Sprintf("%-20s", r.format)
				// Color the format.
				switch {
				case r.format == "empty (all zeros)":
					padFmt = dim(padFmt)
				case strings.Contains(r.format, "unknown"):
					padFmt = dim(padFmt)
				case strings.Contains(r.format, "error"):
					padFmt = red(padFmt)
				}

				// Color the entropy.
				entropyStr := fmt.Sprintf("%-7s", fmt.Sprintf("%.2f", r.entropy))
				switch {
				case r.entropy > 7.9:
					entropyStr = red(entropyStr)
				case r.entropy > 7.0:
					entropyStr = yellow(entropyStr)
				case r.entropy < 0.01:
					entropyStr = dim(entropyStr)
				default:
					entropyStr = green(entropyStr)
				}

				fmt.Printf("  %s %s  0x%-10x  %-6s  %s  %s  %s\n",
					marker, padName, p.StartLBA, dim(p.SizeHuman()),
					padFmt, entropyStr, r.status)
			} else {
				fmt.Printf("  %s %s  0x%-10x  %s\n",
					marker, padName, p.StartLBA, dim(p.SizeHuman()))
			}
		}

		fmt.Println()
		fmt.Printf("  %s partitions  %s = slot A  %s = slot B\n",
			bold(fmt.Sprintf("%d", len(partitions.VeloCore))),
			cyan("▪"), green("▪"))
		if hasResults {
			fmt.Println()
			fmt.Printf("  %s  %s  %s\n",
				green("low")+"=clear",
				yellow("mid")+"=compressed",
				red("high")+"=encrypted/random")
		}
		fmt.Println()
		return nil
	},
}

// identifyFormat checks magic bytes to identify the partition contents.
func identifyFormat(data []byte) string {
	if len(data) < 512 {
		return "unknown"
	}

	// Check at offset 0.
	if len(data) >= 4 {
		magic4 := string(data[0:4])
		magic8 := ""
		if len(data) >= 8 {
			magic8 = string(data[0:8])
		}

		// AVB (Android Verified Boot) metadata.
		if magic4 == "AVB0" {
			return "avb (verified boot)"
		}

		// Android boot image.
		if magic8 == "ANDROID!" {
			return "android boot image"
		}

		// LUKS encrypted volume.
		if len(data) >= 6 && string(data[0:6]) == "LUKS\xba\xbe" {
			return "LUKS encrypted"
		}

		// Rockchip loader/resource.
		if magic4 == "RKLD" || magic4 == "BOOT" {
			return "rockchip loader"
		}
		if magic4 == "RSCE" {
			return "rockchip resource"
		}

		// Rockchip "LOADER" packed format (uboot partitions).
		// Magic is at offset 0: "LOADER  " (8 bytes, padded with spaces).
		if len(data) >= 8 && string(data[0:6]) == "LOADER" {
			return "rk uboot (packed)"
		}

		// Rockchip trust image — BL31 header ("BL3X").
		if magic4 == "BL3X" {
			return "rk trust (BL31)"
		}

		// ARM64 branch instruction (ATF binary without header).
		if len(data) >= 8 {
			instr := binary.LittleEndian.Uint32(data[0:4])
			if instr&0xFC000000 == 0x14000000 && instr != 0 {
				return "arm64 binary (ATF)"
			}
		}

		// FIT image (flattened image tree / U-Boot).
		if binary.BigEndian.Uint32(data[0:4]) == 0xd00dfeed {
			return "FIT/DTB"
		}

		// Device tree blob.
		if binary.BigEndian.Uint32(data[0:4]) == 0xedfe0dd0 {
			return "DTB (device tree)"
		}

		// Android sparse image.
		if binary.LittleEndian.Uint32(data[0:4]) == 0xED26FF3A {
			return "android sparse"
		}

		// SquashFS.
		if binary.LittleEndian.Uint32(data[0:4]) == 0x73717368 {
			return "squashfs"
		}

		// GPT protective MBR.
		if len(data) >= 512 && data[510] == 0x55 && data[511] == 0xAA {
			return "MBR/GPT"
		}

		_ = magic8
	}

	// F2FS superblock at offset 0x400 (1024).
	if len(data) >= 0x404 {
		if binary.LittleEndian.Uint32(data[0x400:0x404]) == 0xF2F52010 {
			return "f2fs"
		}
	}

	// ext4 superblock at offset 0x438 (1080).
	if len(data) >= 0x43A {
		if binary.LittleEndian.Uint16(data[0x438:0x43A]) == 0xEF53 {
			return "ext4"
		}
	}

	// Check if all zeros.
	allZero := true
	for _, b := range data {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return "zeroed"
	}

	return "unknown"
}

// shannonEntropy calculates Shannon entropy in bits per byte (0.0 to 8.0).
func shannonEntropy(data []byte) float64 {
	if len(data) == 0 {
		return 0
	}
	var freq [256]int
	for _, b := range data {
		freq[b]++
	}
	n := float64(len(data))
	entropy := 0.0
	for _, count := range freq {
		if count == 0 {
			continue
		}
		p := float64(count) / n
		entropy -= p * math.Log2(p)
	}
	return entropy
}

// assessStatus returns a status label based on format and entropy.
func assessStatus(format string, entropy float64) string {
	switch {
	case format == "LUKS encrypted":
		return red("ENCRYPTED (LUKS)")
	case format == "zeroed":
		return dim("zeroed")
	case format != "unknown":
		if entropy > 7.5 {
			return yellow("compressed/signed")
		}
		return green("clear")
	case entropy > 7.9:
		return red("LIKELY ENCRYPTED")
	case entropy > 7.0:
		return yellow("possibly encrypted")
	case entropy < 1.0:
		return dim("low data")
	default:
		return dim("unrecognized")
	}
}
