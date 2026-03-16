package cmd

import (
	"fmt"

	"github.com/ril3y/velotool/pkg/rockusb"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(detectCmd)
}

var detectCmd = &cobra.Command{
	Use:   "detect",
	Short: "Scan USB for Rockchip devices",
	Long: `Scan USB buses for Rockchip RK3399 devices in Maskrom or Loader mode.

Shows device mode, USB bus/address, and product ID. If a device is found
and accessible, also queries chip and flash information.

` + dim("Tip: If no device is found, ensure the board is powered with the reset") + `
` + dim("button held to enter Maskrom mode, and USB is connected."),
	Example: `  velotool detect
  velotool detect -v`,
	RunE: func(cmd *cobra.Command, args []string) error {
		devices, err := rockusb.Detect()
		if err != nil {
			return fmt.Errorf("USB scan: %w", err)
		}
		if len(devices) == 0 {
			noDeviceHelp()
			return nil
		}

		for _, d := range devices {
			fmt.Println()
			fmt.Printf("  %s %s\n", green("✓"), boldCyan(d.ChipName))
			fmt.Println(dim("  ┌──────────────────────────────────"))
			fmt.Printf("  │  Mode     %s\n", bold(d.Mode.String()))
			fmt.Printf("  │  USB      Bus %d, Address %d\n", d.Bus, d.Address)
			fmt.Printf("  │  PID      0x%04x\n", d.ProductID)
		}

		// Try to open and get chip info.
		dev, err := rockusb.Open()
		if err != nil {
			fmt.Println(dim("  └──────────────────────────────────"))
			return nil // Detection succeeded, just can't open.
		}
		defer dev.Close()
		if logger != nil {
			dev.SetLogger(logger)
		}

		if info, err := dev.ReadChipInfo(); err == nil && len(info) > 0 {
			fmt.Printf("  │  Chip     %x\n", info)
		}
		if info, err := dev.ReadFlashInfo(); err == nil && len(info) > 0 {
			fmt.Printf("  │  Flash    %x\n", info)
		}
		fmt.Println(dim("  └──────────────────────────────────"))
		fmt.Println()

		return nil
	},
}
