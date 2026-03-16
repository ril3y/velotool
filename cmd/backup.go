package cmd

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ril3y/velotool/pkg/gpt"
	"github.com/ril3y/velotool/pkg/partitions"
	"github.com/ril3y/velotool/pkg/rockusb"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

var skipUserdata bool

func init() {
	backupCmd.Flags().BoolVar(&skipUserdata, "skip-userdata", false, "Skip the large userdata partition (~12 GB)")
	rootCmd.AddCommand(backupCmd)
}

// BackupManifest is the JSON manifest written after a full backup.
type BackupManifest struct {
	Device     string            `json:"device"`
	Timestamp  string            `json:"timestamp"`
	GPTSource  string            `json:"gpt_source"`
	Partitions []BackupPartition `json:"partitions"`
}

type BackupPartition struct {
	Name      string `json:"name"`
	StartLBA  uint64 `json:"start_lba"`
	SizeLBA   uint64 `json:"size_lba"`
	SizeBytes uint64 `json:"size_bytes"`
	SHA256    string `json:"sha256"`
	File      string `json:"file"`
}

var backupCmd = &cobra.Command{
	Use:   "backup <output_dir>",
	Short: "Full device backup with GPT discovery and SHA256 manifest",
	Long: `Dump all eMMC partitions to individual .img files in the output directory.

First attempts to read the GPT from the device (LBA 1-33). If GPT is not
available, falls back to the embedded partition table. Each partition is
dumped with a SHA256 checksum. A JSON manifest and checksums.sha256 file
are written at the end.

Use --skip-userdata to skip the ~12 GB userdata partition (F2FS) which
is usually not needed for firmware restoration.`,
	Example: `  velotool backup ./my_backup/
  velotool backup ./my_backup/ --skip-userdata
  velotool backup ./my_backup/ -y`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		outDir := args[0]

		fmt.Println()
		fmt.Printf("  %s\n", boldCyan("VeloCore RK3399 — Full Device Backup"))
		fmt.Println()

		if err := os.MkdirAll(outDir, 0755); err != nil {
			return fmt.Errorf("create output dir: %w", err)
		}

		// Open device (auto-sends loader if in maskrom mode).
		dev, err := openDevice()
		if err != nil {
			return err
		}
		defer dev.Close()

		// Try to read GPT from device.
		var parts []partitions.Partition
		gptSource := "embedded"

		fmt.Printf("  %s Reading GPT from device...\n", dim("⟳"))
		gptHeader, err := dev.ReadLBA(1, 1)
		if err == nil {
			gptEntries, err := dev.ReadLBA(2, 32)
			if err == nil {
				table, err := gpt.Parse(gptHeader, gptEntries)
				if err == nil {
					fmt.Printf("  %s %d partitions found in GPT\n", green("✓"), len(table.Entries))
					gptSource = "device"
					for _, e := range table.Entries {
						parts = append(parts, partitions.Partition{
							Name:     e.Name,
							StartLBA: e.StartLBA,
							SizeLBA:  e.SizeLBA(),
						})
					}
					// Save raw GPT.
					gptFile := filepath.Join(outDir, "gpt.bin")
					raw := append(gptHeader, gptEntries...)
					if err := os.WriteFile(gptFile, raw, 0644); err != nil {
						fmt.Printf("  %s could not save GPT: %v\n", yellow("⚠"), err)
					}
				}
			}
		}

		if gptSource == "embedded" {
			fmt.Printf("  %s Could not read GPT, using embedded partition table\n", yellow("⚠"))
			parts = partitions.VeloCore
		}

		// Calculate total size needed and filter skipped partitions.
		var totalNeeded uint64
		var dumpParts []partitions.Partition
		for _, p := range parts {
			if skipUserdata && p.Name == "userdata" {
				continue
			}
			dumpParts = append(dumpParts, p)
			totalNeeded += p.SizeBytes()
		}

		// Check free disk space.
		absDir, err := filepath.Abs(outDir)
		if err != nil {
			absDir = outDir
		}

		fmt.Println()
		fmt.Printf("  %s  %s\n", dim("Backup to"), bold(absDir))

		if freeBytes, err := freeSpace(absDir); err == nil {
			freeStr := formatBytes(int64(freeBytes))
			if freeBytes >= totalNeeded {
				freeStr = green(freeStr)
			} else {
				freeStr = red(freeStr)
			}
			fmt.Printf("  %s  %s needed, %s available\n",
				dim("Space   "),
				cyan(formatBytes(int64(totalNeeded))),
				freeStr)

			if freeBytes < totalNeeded {
				fmt.Println()
				fmt.Printf("  %s Not enough disk space! Need %s more.\n",
					red("✗"), red(formatBytes(int64(totalNeeded-freeBytes))))
				fmt.Println()
				return fmt.Errorf("insufficient disk space")
			}
		}

		fmt.Printf("  %s  %d partitions (%d total%s)\n",
			dim("Dump    "),
			len(dumpParts), len(parts),
			func() string {
				if skipUserdata {
					return ", userdata skipped"
				}
				return ""
			}())
		fmt.Println()

		if !confirm(fmt.Sprintf("  Begin full backup to %s?", absDir)) {
			fmt.Println("  Aborted.")
			return nil
		}

		manifest := BackupManifest{
			Device:    "RK3399",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			GPTSource: gptSource,
		}

		var checksumLines []string
		total := len(parts)
		skipped := 0
		startTime := time.Now()

		for i, p := range parts {
			if skipUserdata && p.Name == "userdata" {
				fmt.Printf("\n  %s %s %s\n",
					dim(fmt.Sprintf("[%d/%d]", i+1, total)),
					yellow("Skipping"),
					yellow(p.Name))
				skipped++
				continue
			}

			filename := p.Name + ".img"
			outPath := filepath.Join(outDir, filename)
			totalBytes := int64(p.SizeLBA) * rockusb.SectorSize

			fmt.Printf("\n  %s %s  LBA 0x%x  %s\n",
				bold(fmt.Sprintf("[%d/%d]", i+1, total)),
				boldCyan(p.Name),
				p.StartLBA,
				dim(p.SizeHuman()))

			f, err := os.Create(outPath)
			if err != nil {
				return fmt.Errorf("create %s: %w", filename, err)
			}

			hasher := sha256.New()
			w := io.MultiWriter(f, hasher)

			bar := progressbar.NewOptions64(
				totalBytes,
				progressbar.OptionSetDescription("    Reading"),
				progressbar.OptionSetWidth(30),
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

			if err := dev.ReadToWriter(w, p.StartLBA, p.SizeLBA, cfg); err != nil {
				f.Close()
				return fmt.Errorf("read %s: %w", p.Name, err)
			}
			f.Close()
			bar.Finish()

			hash := fmt.Sprintf("%x", hasher.Sum(nil))
			fmt.Printf("    %s %s\n", dim("SHA-256"), dim(hash))

			manifest.Partitions = append(manifest.Partitions, BackupPartition{
				Name:      p.Name,
				StartLBA:  p.StartLBA,
				SizeLBA:   p.SizeLBA,
				SizeBytes: uint64(totalBytes),
				SHA256:    hash,
				File:      filename,
			})
			checksumLines = append(checksumLines, fmt.Sprintf("%s  %s", hash, filename))
		}

		// Write manifest.
		manifestPath := filepath.Join(outDir, "manifest.json")
		mjson, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal manifest: %w", err)
		}
		if err := os.WriteFile(manifestPath, mjson, 0644); err != nil {
			return fmt.Errorf("write manifest: %w", err)
		}

		// Write checksums.sha256.
		checksumPath := filepath.Join(outDir, "checksums.sha256")
		checksumContent := strings.Join(checksumLines, "\n") + "\n"
		if err := os.WriteFile(checksumPath, []byte(checksumContent), 0644); err != nil {
			return fmt.Errorf("write checksums: %w", err)
		}

		elapsed := time.Since(startTime).Round(time.Second)
		fmt.Println()
		fmt.Printf("  %s Backup complete — %d partitions", green("✓"), len(manifest.Partitions))
		if skipped > 0 {
			fmt.Printf(" (%d skipped)", skipped)
		}
		fmt.Printf(" in %s\n", bold(elapsed.String()))
		fmt.Println()
		fmt.Printf("  %s  %s\n", dim("Manifest "), manifestPath)
		fmt.Printf("  %s  %s\n", dim("Checksums"), checksumPath)
		fmt.Println()
		return nil
	},
}
