package cmd

import (
	_ "embed"
	"fmt"
	"log"
	"os"

	"github.com/fatih/color"
	"github.com/ril3y/velotool/pkg/rockusb"
	"github.com/spf13/cobra"
)

//go:embed loader.bin
var embeddedLoader []byte

var (
	verbose bool
	yes     bool
	logger  *log.Logger
)

// Color helpers for consistent styling.
var (
	bold      = color.New(color.Bold).SprintFunc()
	boldCyan  = color.New(color.Bold, color.FgCyan).SprintFunc()
	boldGreen = color.New(color.Bold, color.FgGreen).SprintFunc()
	green     = color.New(color.FgGreen).SprintFunc()
	yellow    = color.New(color.FgYellow).SprintFunc()
	red       = color.New(color.FgRed).SprintFunc()
	cyan      = color.New(color.FgCyan).SprintFunc()
	magenta   = color.New(color.FgMagenta).SprintFunc()
	dim       = color.New(color.Faint).SprintFunc()
)

const banner = `
 ██╗   ██╗███████╗██╗      ██████╗ ████████╗ ██████╗  ██████╗ ██╗
 ██║   ██║██╔════╝██║     ██╔═══██╗╚══██╔══╝██╔═══██╗██╔═══██╗██║
 ██║   ██║█████╗  ██║     ██║   ██║   ██║   ██║   ██║██║   ██║██║
 ╚██╗ ██╔╝██╔══╝  ██║     ██║   ██║   ██║   ██║   ██║██║   ██║██║
  ╚████╔╝ ███████╗███████╗╚██████╔╝   ██║   ╚██████╔╝╚██████╔╝███████╗
   ╚═══╝  ╚══════╝╚══════╝ ╚═════╝    ╚═╝    ╚═════╝  ╚═════╝ ╚══════╝`

func colorBanner() string {
	c := color.New(color.FgCyan, color.Bold)
	return c.Sprint(banner)
}

var rootCmd = &cobra.Command{
	Use:   "velotool",
	Short: "VeloCore RK3399 flash tool",
	Long: colorBanner() + `
` + dim("  Cross-platform flash tool for the Bowflex VeloCore (RK3399)") + `
` + dim("  www.battlewithbytes.io") + `

  Reads, writes, and backs up eMMC partitions over USB using the
  Rockchip maskrom/loader protocol. Embeds the DDR loader binary
  so no separate files are needed` + bold(" — just plug in USB and go.") + `

  ` + yellow("Requirements:") + `
    ` + dim("•") + ` Device in Maskrom or Loader mode (hold reset button)
    ` + dim("•") + ` USB cable connected to host
    ` + dim("•") + ` On Windows: WinUSB driver via Zadig`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if verbose {
			logger = log.New(os.Stderr, cyan("[velotool] "), log.LstdFlags)
		}
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose USB protocol logging")
	rootCmd.PersistentFlags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompts (use with caution)")
}

func Execute() error {
	return rootCmd.Execute()
}

func confirm(prompt string) bool {
	if yes {
		return true
	}
	fmt.Printf("%s [y/N]: ", yellow(prompt))
	var resp string
	fmt.Scanln(&resp)
	return resp == "y" || resp == "Y" || resp == "yes"
}

// noDeviceHelp prints troubleshooting steps when no device is found.
func noDeviceHelp() {
	fmt.Println()
	fmt.Printf("  %s No Rockchip device found on USB.\n", red("✗"))
	fmt.Println()
	fmt.Printf("  %s\n", bold("To enter Maskrom mode:"))
	fmt.Printf("    1. Connect the USB cable between the device and this host\n")
	fmt.Printf("    2. Hold the %s button on the board\n", bold("recovery"))
	fmt.Printf("    3. While holding recovery, press and release the %s button\n", bold("reset"))
	fmt.Printf("    4. Continue holding recovery for %s, then release\n", bold("~5 seconds"))
	fmt.Printf("    5. Run %s to verify the device is detected\n", cyan("velotool detect"))
	fmt.Println()
	fmt.Printf("  %s\n", dim("On Windows, the Rockchip device also needs the WinUSB driver"))
	fmt.Printf("  %s\n", dim("installed via Zadig (zadig.akeo.ie)."))
	fmt.Println()
}

// openDevice opens a Rockchip device and, if it's in maskrom mode,
// automatically downloads the embedded loader to initialize DDR and USB plug.
// Returns a device in loader mode ready for LBA operations.
func openDevice() (*rockusb.Device, error) {
	dev, err := rockusb.Open()
	if err != nil {
		if err == rockusb.ErrDeviceNotFound {
			noDeviceHelp()
		}
		return nil, err
	}
	if logger != nil {
		dev.SetLogger(logger)
	}

	if dev.Info.Mode == rockusb.ModeMaskrom {
		// In maskrom mode, download the DDR loader + USB plug.
		// Do NOT probe with bulk TestUnitReady first — sending CBW packets
		// to the boot ROM's bulk endpoint corrupts USB state and causes
		// subsequent control transfers to fail with error -1.
		fmt.Println(yellow("  ⟳ ") + "Maskrom mode — sending DDR loader...")
		newDev, err := rockusb.DownloadBoot(dev, embeddedLoader)
		if err != nil {
			dev.Close()
			return nil, fmt.Errorf("download boot: %w", err)
		}
		if logger != nil {
			newDev.SetLogger(logger)
		}
		fmt.Printf("%s Loader active — %s mode\n", green("  ✓"), bold(newDev.Info.Mode.String()))
		return newDev, nil
	}

	fmt.Printf("%s Device in %s mode\n", green("  ✓"), bold(dev.Info.Mode.String()))
	return dev, nil
}
