package cmd

import (
	"fmt"

	"github.com/ril3y/velotool/pkg/rockusb"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(resetCmd)
}

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset the device (reboot)",
	Long: `Send a USB reset command to the Rockchip device.

This causes the device to reboot. After reset, the device will go through
its normal boot sequence (BootROM → loader → U-Boot → Android).`,
	Example: "  velotool reset",
	RunE: func(cmd *cobra.Command, args []string) error {
		dev, err := rockusb.Open()
		if err != nil {
			return err
		}
		defer dev.Close()
		if logger != nil {
			dev.SetLogger(logger)
		}

		fmt.Println("Resetting device...")
		if err := dev.Reset(0); err != nil {
			return fmt.Errorf("reset: %w", err)
		}
		fmt.Println(green("Device reset sent."))
		return nil
	},
}
