package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ril3y/velotool/pkg/partitions"
	"github.com/ril3y/velotool/pkg/rockusb"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(flashAllCmd)
}

type flashEntry struct {
	partition partitions.Partition
	path      string
	filename  string
	size      int64
}

var flashAllCmd = &cobra.Command{
	Use:   "flash-all <manifest>",
	Short: "Flash multiple partitions from a manifest file",
	Long: `Flash multiple partition images in sequence using a manifest file.

The manifest is a text file with one entry per line:

    <partition_name>  <image_file>

Blank lines and lines starting with # are ignored. Image file paths are
resolved relative to the directory containing the manifest file.

All entries are validated (partition names and file existence) before any
writes begin.`,
	Example: `  # Create a manifest file (flash.txt):
  #
  #   uboot_b   uboot_b_patched.img
  #   system_b  system_b.img
  #   vendor_b  vendor_b.img
  #
  # Then flash everything:
  velotool flash-all flash.txt
  velotool flash-all flash.txt -y   # skip confirmation`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		manifestPath := args[0]

		// Parse manifest file.
		entries, err := parseManifest(manifestPath)
		if err != nil {
			return err
		}

		if len(entries) == 0 {
			fmt.Println()
			fmt.Printf("  %s No entries found in %s\n", red("✗"), manifestPath)
			fmt.Println()
			return nil
		}

		fmt.Printf("\n  %s\n\n", boldCyan("Flash All — "+filepath.Base(manifestPath)))

		for _, e := range entries {
			fmt.Printf("  %s → %s (%s, LBA 0x%x)\n",
				e.filename, cyan(e.partition.Name), dim(formatBytes(e.size)), e.partition.StartLBA)
		}

		if !confirm(fmt.Sprintf("\n  Flash %d partitions?", len(entries))) {
			fmt.Println("  Aborted.")
			return nil
		}

		// Open device (auto-sends loader if in maskrom mode).
		dev, err := openDevice()
		if err != nil {
			return err
		}
		defer dev.Close()

		for i, e := range entries {
			f, err := os.Open(e.path)
			if err != nil {
				return fmt.Errorf("open %s: %w", e.filename, err)
			}

			fileSectors := uint64((e.size + rockusb.SectorSize - 1) / rockusb.SectorSize)

			fmt.Printf("\n  %s %s → %s\n",
				bold(fmt.Sprintf("[%d/%d]", i+1, len(entries))),
				e.filename, cyan(e.partition.Name))

			bar := progressbar.NewOptions64(
				e.size,
				progressbar.OptionSetDescription(fmt.Sprintf("    %s", e.partition.Name)),
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

			if err := dev.WriteFromReader(f, e.partition.StartLBA, fileSectors, cfg); err != nil {
				f.Close()
				return fmt.Errorf("flash %s: %w", e.partition.Name, err)
			}
			f.Close()
			bar.Finish()
		}

		fmt.Printf("\n  %s All %d partitions flashed.\n\n", green("✓"), len(entries))
		return nil
	},
}

// parseManifest reads a flash manifest file and validates all entries.
// Each line: <partition_name> <image_file>
// Blank lines and lines starting with # are ignored.
// Image paths are resolved relative to the manifest file's directory.
func parseManifest(path string) ([]flashEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open manifest: %w", err)
	}
	defer f.Close()

	baseDir := filepath.Dir(path)
	var entries []flashEntry

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip blank lines and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("%s:%d: expected '<partition> <image_file>', got: %s", path, lineNum, line)
		}

		partName := fields[0]
		imgFile := fields[1]

		// Validate partition name.
		part, err := partitions.Lookup(partName)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNum, err)
		}

		// Resolve image path relative to manifest directory.
		imgPath := imgFile
		if !filepath.IsAbs(imgPath) {
			imgPath = filepath.Join(baseDir, imgFile)
		}

		fi, err := os.Stat(imgPath)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: image not found: %s", path, lineNum, imgPath)
		}

		entries = append(entries, flashEntry{
			partition: *part,
			path:      imgPath,
			filename:  imgFile,
			size:      fi.Size(),
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	return entries, nil
}
