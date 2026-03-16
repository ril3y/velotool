package cmd

import (
	"fmt"
	"os"

	"github.com/ril3y/velotool/pkg/partitions"
	"github.com/ril3y/velotool/pkg/rockusb"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

var (
	readLBA     uint64
	readSectors uint64
)

func init() {
	readCmd.Flags().Uint64Var(&readLBA, "lba", 0, "Override start LBA (instead of partition name)")
	readCmd.Flags().Uint64Var(&readSectors, "sectors", 0, "Number of sectors to read (required with --lba)")
	rootCmd.AddCommand(readCmd)
}

var readCmd = &cobra.Command{
	Use:   "read <partition> <file>",
	Short: "Read a partition to a local file",
	Long: `Read the contents of an eMMC partition to a local file.

The partition can be specified by name (e.g. "uboot_b", "vbmeta_b") using the
embedded partition table, or by raw LBA offset using --lba and --sectors.

Data is read in 64KB chunks (128 sectors) over USB. A progress bar shows
transfer status. The resulting file can be verified with sha256sum.`,
	Example: `  velotool read vbmeta_b vbmeta_backup.img
  velotool read uboot_b uboot_b_dump.img
  velotool read --lba 0x6000 --sectors 8192 custom_dump.img dummy`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		partName := args[0]
		filePath := args[1]

		fmt.Println()

		// Resolve partition.
		var startLBA, sizeLBA uint64
		if readLBA != 0 {
			startLBA = readLBA
			sizeLBA = readSectors
			if sizeLBA == 0 {
				return fmt.Errorf("--sectors is required when using --lba")
			}
		} else {
			part, err := partitions.Lookup(partName)
			if err != nil {
				return err
			}
			startLBA = part.StartLBA
			sizeLBA = part.SizeLBA
			fmt.Printf("  %s  %s\n", dim("Partition"), boldCyan(part.Name))
			fmt.Printf("  %s  0x%x (%s)\n", dim("LBA      "), startLBA, cyan(part.SizeHuman()))
		}

		totalBytes := int64(sizeLBA) * rockusb.SectorSize
		fmt.Printf("  %s  %d sectors (%s)\n", dim("Read     "), sizeLBA, cyan(formatBytes(totalBytes)))
		fmt.Printf("  %s  %s\n", dim("Output   "), filePath)
		fmt.Println()

		// Open output file.
		f, err := os.Create(filePath)
		if err != nil {
			return fmt.Errorf("create file: %w", err)
		}
		defer f.Close()

		// Open device (auto-sends loader if in maskrom mode).
		dev, err := openDevice()
		if err != nil {
			return err
		}
		defer dev.Close()

		fmt.Println()

		// Create progress bar.
		bar := progressbar.NewOptions64(
			totalBytes,
			progressbar.OptionSetDescription("  Reading"),
			progressbar.OptionSetWidth(40),
			progressbar.OptionShowBytes(true),
			progressbar.OptionShowCount(),
			progressbar.OptionSetTheme(progressbar.Theme{
				Saucer:        green("━"),
				SaucerHead:    green("╸"),
				SaucerPadding: dim("━"),
				BarStart:      dim("╺"),
				BarEnd:        dim("╸"),
			}),
			progressbar.OptionOnCompletion(func() { fmt.Println() }),
		)

		cfg := rockusb.DefaultTransferConfig()
		cfg.OnProgress = func(current, total int64) {
			bar.Set64(current)
		}

		if err := dev.ReadToWriter(f, startLBA, sizeLBA, cfg); err != nil {
			return fmt.Errorf("\n  read failed: %w", err)
		}

		bar.Finish()
		fmt.Println()
		fmt.Printf("  %s %s → %s\n\n", green("✓"), boldCyan(partName), filePath)
		return nil
	},
}

// formatBytes returns a human-readable byte count.
func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
