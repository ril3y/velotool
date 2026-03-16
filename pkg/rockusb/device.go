package rockusb

import (
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/google/gousb"
)

// Rockchip USB Vendor ID.
const VendorRockchip = 0x2207

// Known Rockchip product IDs.
var KnownProducts = map[gousb.ID]string{
	0x330C: "RK3399 (Maskrom)",
	0x330D: "RK3399 (Loader)",
}

// DeviceMode indicates whether the device is in maskrom or loader mode.
type DeviceMode int

const (
	ModeMaskrom DeviceMode = iota
	ModeLoader
	ModeUnknown
)

func (m DeviceMode) String() string {
	switch m {
	case ModeMaskrom:
		return "Maskrom"
	case ModeLoader:
		return "Loader"
	default:
		return "Unknown"
	}
}

// DeviceInfo contains information about a discovered Rockchip device.
type DeviceInfo struct {
	Bus       int
	Address   int
	ProductID gousb.ID
	Mode      DeviceMode
	ChipName  string
}

// Device represents an open connection to a Rockchip device.
type Device struct {
	ctx    *gousb.Context
	dev    *gousb.Device
	intf   *gousb.Interface
	inEP   *gousb.InEndpoint
	outEP  *gousb.OutEndpoint
	done   func()
	Info   DeviceInfo
	tag    atomic.Uint32
	logger *log.Logger
}

// SetLogger sets a logger for verbose output.
func (d *Device) SetLogger(l *log.Logger) {
	d.logger = l
}

func (d *Device) logf(format string, args ...interface{}) {
	if d.logger != nil {
		d.logger.Printf(format, args...)
	}
}

// nextTag returns the next unique CBW tag.
func (d *Device) nextTag() uint32 {
	return d.tag.Add(1)
}

// Detect finds all Rockchip devices on USB without opening them.
func Detect() ([]DeviceInfo, error) {
	ctx := gousb.NewContext()
	defer ctx.Close()

	var devices []DeviceInfo
	devs, err := ctx.OpenDevices(func(desc *gousb.DeviceDesc) bool {
		return desc.Vendor == VendorRockchip
	})
	for _, dev := range devs {
		desc := dev.Desc
		name, ok := KnownProducts[desc.Product]
		if !ok {
			name = fmt.Sprintf("Unknown (0x%04x)", desc.Product)
		}
		mode := modeFromPID(desc.Product)
		devices = append(devices, DeviceInfo{
			Bus:       desc.Bus,
			Address:   desc.Address,
			ProductID: desc.Product,
			Mode:      mode,
			ChipName:  name,
		})
		dev.Close()
	}
	if err != nil {
		return devices, err
	}
	return devices, nil
}

func modeFromPID(pid gousb.ID) DeviceMode {
	switch pid {
	case 0x330C:
		return ModeMaskrom
	case 0x330D:
		return ModeLoader
	default:
		return ModeUnknown
	}
}

// Open connects to the first Rockchip device found.
func Open() (*Device, error) {
	ctx := gousb.NewContext()

	// Try all known product IDs.
	var dev *gousb.Device
	var err error
	for pid := range KnownProducts {
		dev, err = ctx.OpenDeviceWithVIDPID(VendorRockchip, pid)
		if err == nil && dev != nil {
			break
		}
	}
	if dev == nil {
		ctx.Close()
		if err != nil {
			return nil, fmt.Errorf("USB open: %w", err)
		}
		return nil, ErrDeviceNotFound
	}

	return openDevice(ctx, dev)
}

// OpenWithPID connects to a Rockchip device with a specific product ID.
func OpenWithPID(pid gousb.ID) (*Device, error) {
	ctx := gousb.NewContext()

	dev, err := ctx.OpenDeviceWithVIDPID(VendorRockchip, pid)
	if err != nil {
		ctx.Close()
		return nil, fmt.Errorf("USB open: %w", err)
	}
	if dev == nil {
		ctx.Close()
		return nil, ErrDeviceNotFound
	}

	return openDevice(ctx, dev)
}

func openDevice(ctx *gousb.Context, dev *gousb.Device) (*Device, error) {
	if err := dev.SetAutoDetach(true); err != nil {
		dev.Close()
		ctx.Close()
		return nil, fmt.Errorf("auto-detach: %w", err)
	}

	desc := dev.Desc
	name, ok := KnownProducts[desc.Product]
	if !ok {
		name = fmt.Sprintf("Unknown (0x%04x)", desc.Product)
	}
	mode := modeFromPID(desc.Product)

	d := &Device{
		ctx: ctx,
		dev: dev,
		Info: DeviceInfo{
			Bus:       desc.Bus,
			Address:   desc.Address,
			ProductID: desc.Product,
			Mode:      mode,
			ChipName:  name,
		},
	}
	d.tag.Store(0)

	// In raw maskrom mode (before loader download), we skip bulk endpoint
	// claiming — the boot ROM only accepts control transfers.
	// After loader download, the caller must call EnsureBulkEndpoints().
	if mode != ModeMaskrom {
		if err := d.claimBulkEndpoints(); err != nil {
			dev.Close()
			ctx.Close()
			return nil, err
		}
	}

	return d, nil
}

// claimBulkEndpoints claims the interface and finds bulk IN/OUT endpoints.
func (d *Device) claimBulkEndpoints() error {
	intf, done, err := d.dev.DefaultInterface()
	if err != nil {
		return fmt.Errorf("claim interface: %w", err)
	}

	var inEP *gousb.InEndpoint
	var outEP *gousb.OutEndpoint
	for _, ep := range intf.Setting.Endpoints {
		if ep.Direction == gousb.EndpointDirectionIn {
			inEP, err = intf.InEndpoint(ep.Number)
			if err != nil {
				done()
				return fmt.Errorf("in endpoint: %w", err)
			}
		} else {
			outEP, err = intf.OutEndpoint(ep.Number)
			if err != nil {
				done()
				return fmt.Errorf("out endpoint: %w", err)
			}
		}
	}
	if inEP == nil || outEP == nil {
		done()
		return fmt.Errorf("device missing bulk endpoints")
	}

	d.intf = intf
	d.done = done
	d.inEP = inEP
	d.outEP = outEP
	d.logf("IN endpoint: %s (maxpkt=%d)", inEP.String(), inEP.Desc.MaxPacketSize)
	d.logf("OUT endpoint: %s (maxpkt=%d)", outEP.String(), outEP.Desc.MaxPacketSize)
	return nil
}

// EnsureBulkEndpoints claims bulk endpoints if not already claimed.
// Called after transitioning from maskrom to loader mode.
func (d *Device) EnsureBulkEndpoints() error {
	if d.inEP != nil && d.outEP != nil {
		return nil
	}
	return d.claimBulkEndpoints()
}

// Close releases all USB resources.
func (d *Device) Close() {
	if d.done != nil {
		d.done()
	}
	if d.dev != nil {
		d.dev.Close()
	}
	if d.ctx != nil {
		d.ctx.Close()
	}
}

// ctrlTimeout is the timeout for control transfers in milliseconds.
// 0 = unlimited, matching rkdeveloptool's CMD_TIMEOUT=0.
// The boot ROM blocks USB while executing DDR init code, so the CRC
// transfer won't complete until execution finishes (can take 5-10s).
const ctrlTimeout = 0

// ControlTransfer sends a USB control transfer (used for maskrom boot download).
// Uses direct libusb with explicit timeout to avoid blocking forever when the
// boot ROM is executing code (DDR init / USB plug).
func (d *Device) ControlTransfer(bmRequestType, bRequest uint8, wValue, wIndex uint16, data []byte) (int, error) {
	return d.controlTransfer(bmRequestType, bRequest, wValue, wIndex, data, ctrlTimeout)
}

// sendCommand sends a CBW, optional data, and reads the CSW.
// Only works in loader mode (requires bulk endpoints).
func (d *Device) sendCommand(cdb [16]byte, direction byte, data []byte) ([]byte, error) {
	if d.outEP == nil || d.inEP == nil {
		return nil, fmt.Errorf("bulk endpoints not available (device in maskrom mode?)")
	}

	inAddr := byte(d.inEP.Desc.Address)
	outAddr := byte(d.outEP.Desc.Address)

	tag := d.nextTag()
	dataLen := uint32(len(data))
	cbw := NewCBW(tag, dataLen, direction, cdb)

	d.logf("CBW: tag=0x%08x op=0x%02x dir=0x%02x len=%d (in=0x%02x out=0x%02x)",
		tag, cdb[0], direction, dataLen, inAddr, outAddr)

	// Send CBW via direct libusb_bulk_transfer.
	if err := d.bulkWrite(outAddr, cbw.Encode()); err != nil {
		return nil, fmt.Errorf("CBW write: %w", err)
	}

	var result []byte

	if direction == DirectionOut && len(data) > 0 {
		// Send data.
		if err := d.bulkWrite(outAddr, data); err != nil {
			return nil, fmt.Errorf("data write: %w", err)
		}
	} else if direction == DirectionIn && dataLen > 0 {
		// Read data via direct libusb_bulk_transfer.
		d.logf("Data phase: reading %d bytes via libusb_bulk_transfer", dataLen)
		var err error
		result, err = d.bulkRead(inAddr, int(dataLen))
		if err != nil {
			return nil, fmt.Errorf("data read: %w", err)
		}
		d.logf("Data read: requested=%d got=%d", dataLen, len(result))
	}

	// Read CSW via direct libusb_bulk_transfer.
	cswBuf, err := d.bulkRead(inAddr, CSWSize)
	if err != nil {
		return result, fmt.Errorf("CSW read: %w", err)
	}
	d.logf("CSW raw read: %d bytes", len(cswBuf))
	csw, err := DecodeCSW(cswBuf)
	if err != nil {
		return result, err
	}
	if csw.Status != 0 {
		return result, &ErrCSWFailed{Tag: csw.Tag, Status: csw.Status}
	}
	d.logf("CSW: tag=0x%08x status=%d residue=%d", csw.Tag, csw.Status, csw.Residue)
	return result, nil
}

// TestUnitReady checks if the device is ready.
func (d *Device) TestUnitReady() error {
	cdb := BuildTestUnitReadyCDB()
	_, err := d.sendCommand(cdb, DirectionIn, nil)
	return err
}

// ReadChipInfo reads chip information from the device.
func (d *Device) ReadChipInfo() ([]byte, error) {
	cdb := BuildReadChipInfoCDB()
	return d.sendCommand(cdb, DirectionIn, make([]byte, 16))
}

// ReadFlashInfo reads flash information from the device.
func (d *Device) ReadFlashInfo() ([]byte, error) {
	cdb := BuildReadFlashInfoCDB()
	return d.sendCommand(cdb, DirectionIn, make([]byte, 11))
}

// ReadFlashID reads the flash ID.
func (d *Device) ReadFlashID() ([]byte, error) {
	cdb := BuildReadFlashIDCDB()
	return d.sendCommand(cdb, DirectionIn, make([]byte, 5))
}

// ReadLBA reads sectors from the device.
func (d *Device) ReadLBA(lba uint32, count uint16) ([]byte, error) {
	cdb := BuildReadLBACDB(lba, count)
	buf := make([]byte, uint32(count)*SectorSize)
	return d.sendCommand(cdb, DirectionIn, buf)
}

// WriteLBA writes sectors to the device.
func (d *Device) WriteLBA(lba uint32, count uint16, data []byte) error {
	cdb := BuildWriteLBACDB(lba, count)
	_, err := d.sendCommand(cdb, DirectionOut, data)
	return err
}

// Reset resets the device.
func (d *Device) Reset(subcode byte) error {
	cdb := BuildResetCDB(subcode)
	_, err := d.sendCommand(cdb, DirectionIn, nil)
	return err
}

// WaitForReconnect polls USB until the device re-enumerates or timeout.
func WaitForReconnect(timeout time.Duration) (*Device, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		dev, err := Open()
		if err == nil {
			return dev, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil, fmt.Errorf("device did not re-enumerate within %v", timeout)
}
