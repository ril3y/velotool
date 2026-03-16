package cmd

import (
	"fmt"
	"os"

	"github.com/ril3y/velotool/pkg/partitions"
	"github.com/ril3y/velotool/pkg/rockusb"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

var flashLBA uint64

func init() {
	flashCmd.Flags().Uint64Var(&flashLBA, "lba", 0, "Override start LBA (instead of partition name)")
	rootCmd.AddCommand(flashCmd)
}

var flashCmd = &cobra.Command{
	Use:   "flash <partition> <file>",
	Short: "Write an image file to a partition",
	Long: `Write a local image file to an eMMC partition.

The image file is written starting at the partition's LBA offset. If the file
is smaller than the partition, only the file's sectors are written. If the
file is larger, the operation is refused.

` + bold("WARNING:") + ` This overwrites data on the device. Use --yes to skip the
confirmation prompt (e.g. in scripts).`,
	Example: `  velotool flash uboot_b uboot_b_patched.img
  velotool flash vbmeta_b vbmeta_b_stock.img -y
  velotool flash --lba 0x6000 custom_uboot.img dummy`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		partName := args[0]
		filePath := args[1]

		fmt.Println()

		// Resolve partition.
		var startLBA, sizeLBA uint64
		if flashLBA != 0 {
			startLBA = flashLBA
			sizeLBA = 0
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

		// Open file.
		f, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("open file: %w", err)
		}
		defer f.Close()

		fi, err := f.Stat()
		if err != nil {
			return fmt.Errorf("stat file: %w", err)
		}
		fileSize := fi.Size()
		fileSectors := uint64((fileSize + rockusb.SectorSize - 1) / rockusb.SectorSize)

		if sizeLBA > 0 && fileSectors > sizeLBA {
			return fmt.Errorf("file (%d sectors) exceeds partition size (%d sectors)", fileSectors, sizeLBA)
		}

		fmt.Printf("  %s  %s (%s, %d sectors)\n", dim("File     "), filePath, cyan(formatBytes(fileSize)), fileSectors)
		fmt.Println()

		if !confirm(fmt.Sprintf("  Write %s → %s (LBA 0x%x)?", filePath, partName, startLBA)) {
			fmt.Println("  Aborted.")
			return nil
		}

		// Open device (auto-sends loader if in maskrom mode).
		dev, err := openDevice()
		if err != nil {
			return err
		}
		defer dev.Close()

		fmt.Println()

		// Create progress bar.
		bar := progressbar.NewOptions64(
			fileSize,
			progressbar.OptionSetDescription("  Flashing"),
			progressbar.OptionSetWidth(40),
			progressbar.OptionShowBytes(true),
			progressbar.OptionShowCount(),
			progressbar.OptionSetTheme(progressbar.Theme{
				Saucer:        yellow("━"),
				SaucerHead:    yellow("╸"),
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

		if err := dev.WriteFromReader(f, startLBA, fileSectors, cfg); err != nil {
			return fmt.Errorf("\n  flash failed: %w", err)
		}

		bar.Finish()
		fmt.Println()
		fmt.Printf("  %s %s → %s (LBA 0x%x)\n\n", green("✓"), filePath, boldCyan(partName), startLBA)
		return nil
	},
}
