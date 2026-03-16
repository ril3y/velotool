package cmd

import (
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(scanCmd)
}

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan partitions for filesystem types and encryption",
	Long: `Alias for "partitions" — scans the device and shows format/encryption info.

Reads the first sectors of every partition to identify contents and
estimate entropy. High entropy with no recognized format suggests encryption.`,
	Example: "  velotool scan",
	RunE:    partitionsCmd.RunE,
}
