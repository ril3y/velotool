package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ril3y/velotool/pkg/partitions"
	"github.com/ril3y/velotool/pkg/rockusb"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(flashAllCmd)
}

var flashAllCmd = &cobra.Command{
	Use:   "flash-all <profile> <dir>",
	Short: "Flash a predefined set of partition images",
	Long: `Flash multiple partition images from a directory using a named profile.

Each profile defines which partitions to flash and which image files to use.
All image files are verified to exist before any writes begin.

Available profiles:
  root-stack      Flash patched U-Boot + rooted system/vendor (slot B)
  stock-restore   Restore all slot B partitions to stock
  full-root       Full root stack including boot and vbmeta`,
	Example: `  velotool flash-all root-stack ./images/
  velotool flash-all stock-restore ./stock_backup/ -y`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		profileName := args[0]
		dir := args[1]

		entries, err := partitions.LookupProfile(profileName)
		if err != nil {
			// Show available profiles on error.
			fmt.Printf("\n%s\n", bold("Available profiles:"))
			for name, ents := range partitions.Profiles {
				fmt.Printf("  %-16s %d partitions\n", cyan(name), len(ents))
			}
			return err
		}

		// Verify all files exist before starting.
		fmt.Printf("Profile: %s (%d partitions)\n\n", bold(profileName), len(entries))
		for _, e := range entries {
			path := filepath.Join(dir, e.Filename)
			fi, err := os.Stat(path)
			if err != nil {
				return fmt.Errorf("missing image: %s", red(path))
			}
			part, err := partitions.Lookup(e.Partition)
			if err != nil {
				return err
			}
			fmt.Printf("  %s → %s (%s, LBA 0x%x)\n",
				e.Filename, cyan(e.Partition), dim(formatBytes(fi.Size())), part.StartLBA)
		}

		if !confirm(fmt.Sprintf("\nFlash %d partitions?", len(entries))) {
			fmt.Println("Aborted.")
			return nil
		}

		// Open device (auto-sends loader if in maskrom mode).
		dev, err := openDevice()
		if err != nil {
			return err
		}
		defer dev.Close()

		for i, e := range entries {
			part, _ := partitions.Lookup(e.Partition)
			path := filepath.Join(dir, e.Filename)

			f, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("open %s: %w", e.Filename, err)
			}

			fi, _ := f.Stat()
			fileSize := fi.Size()
			fileSectors := uint64((fileSize + rockusb.SectorSize - 1) / rockusb.SectorSize)

			fmt.Printf("\n  %s %s → %s\n",
				bold(fmt.Sprintf("[%d/%d]", i+1, len(entries))),
				e.Filename, cyan(e.Partition))

			bar := progressbar.NewOptions64(
				fileSize,
				progressbar.OptionSetDescription(fmt.Sprintf("    %s", e.Partition)),
				progressbar.OptionSetWidth(30),
				progressbar.OptionShowBytes(true),
				progressbar.OptionSetTheme(progressbar.Theme{
					Saucer:        yellow("="),
					SaucerHead:    yellow(">"),
					SaucerPadding: " ",
					BarStart:      "[",
					BarEnd:        "]",
				}),
				progressbar.OptionOnCompletion(func() { fmt.Println() }),
			)

			cfg := rockusb.DefaultTransferConfig()
			cfg.OnProgress = func(current, total int64) {
				bar.Set64(current)
			}

			if err := dev.WriteFromReader(f, part.StartLBA, fileSectors, cfg); err != nil {
				f.Close()
				return fmt.Errorf("flash %s: %w", e.Partition, err)
			}
			f.Close()
			bar.Finish()
		}

		fmt.Printf("\n%s All %d partitions flashed.\n", green("OK"), len(entries))
		return nil
	},
}
