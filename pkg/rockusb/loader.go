package rockusb

import (
	"encoding/binary"
	"fmt"
	"time"
)

// BOOT header constants.
const (
	BootMagic = "BOOT"

	// Control transfer parameters for maskrom boot download.
	CtrlRequestType = 0x40 // Vendor, Host-to-Device
	CtrlRequest     = 0x0C
	CtrlValue       = 0x0000
	CtrlIndex471    = 0x0471 // wIndex for DDR init
	CtrlIndex472    = 0x0472 // wIndex for USB plug
	CtrlChunkSize   = 4096   // 4KB chunks per control transfer
)

// BootEntryGroup describes a group of entries (471, 472, or loader) in the header.
type BootEntryGroup struct {
	Count     byte
	Offset    uint32
	EntrySize byte
}

// BootEntryData describes one code segment parsed from the entry data area.
type BootEntryData struct {
	Type       uint32
	Name       string
	DataOffset uint32
	DataSize   uint32
	DataDelay  uint32
}

// BootHeader is a parsed BOOT header from a Rockchip loader binary.
type BootHeader struct {
	Size     uint16
	Version  uint32
	Entry471 BootEntryGroup
	Entry472 BootEntryGroup
	Rc4Flag  byte
}

// ParseBootHeader parses a Rockchip BOOT header from the loader binary.
func ParseBootHeader(data []byte) (*BootHeader, error) {
	if len(data) < 0x2d {
		return nil, &ErrLoaderParse{Msg: fmt.Sprintf("data too short: %d", len(data))}
	}

	tag := string(data[0:4])
	if tag != BootMagic {
		return nil, &ErrLoaderParse{Msg: fmt.Sprintf("bad magic: %q", tag)}
	}

	hdr := &BootHeader{
		Size:    binary.LittleEndian.Uint16(data[4:6]),
		Version: binary.LittleEndian.Uint32(data[6:10]),
	}

	// Entry 471 (DDR init) at offset 0x19: count(1) + offset(4) + entrySize(1)
	hdr.Entry471.Count = data[0x19]
	hdr.Entry471.Offset = binary.LittleEndian.Uint32(data[0x1a:0x1e])
	hdr.Entry471.EntrySize = data[0x1e]

	// Entry 472 (USB plug) at offset 0x1f
	hdr.Entry472.Count = data[0x1f]
	hdr.Entry472.Offset = binary.LittleEndian.Uint32(data[0x20:0x24])
	hdr.Entry472.EntrySize = data[0x24]

	// Flags at offset 0x2b-0x2c
	hdr.Rc4Flag = data[0x2c]

	return hdr, nil
}

// ParseEntryData parses a RKBOOT_ENTRY structure (57 bytes) from the loader binary.
func ParseEntryData(data []byte) (*BootEntryData, error) {
	if len(data) < 57 {
		return nil, &ErrLoaderParse{Msg: fmt.Sprintf("entry data too short: %d", len(data))}
	}

	entry := &BootEntryData{
		Type:       binary.LittleEndian.Uint32(data[1:5]),
		DataOffset: binary.LittleEndian.Uint32(data[45:49]),
		DataSize:   binary.LittleEndian.Uint32(data[49:53]),
		DataDelay:  binary.LittleEndian.Uint32(data[53:57]),
	}

	// Name is UTF-16LE at offset 5, 40 bytes (20 wchars).
	nameBytes := data[5:45]
	runes := make([]rune, 0, 20)
	for i := 0; i < len(nameBytes)-1; i += 2 {
		ch := binary.LittleEndian.Uint16(nameBytes[i : i+2])
		if ch == 0 {
			break
		}
		runes = append(runes, rune(ch))
	}
	entry.Name = string(runes)

	return entry, nil
}

// crcCCITT computes CRC-CCITT (0xFFFF initial, polynomial 0x1021).
func crcCCITT(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// DownloadBoot uploads a loader binary (DDR init + USB plug) to the device
// using USB control transfers and executes it. The device will re-enumerate afterward.
func DownloadBoot(dev *Device, loaderData []byte) (*Device, error) {
	hdr, err := ParseBootHeader(loaderData)
	if err != nil {
		return nil, err
	}

	rc4 := hdr.Rc4Flag != 0
	dev.logf("Loader: version=0x%08x, rc4=%v", hdr.Version, rc4)

	// Download 471 entries (DDR init) via control transfers.
	for i := byte(0); i < hdr.Entry471.Count; i++ {
		off := hdr.Entry471.Offset + uint32(i)*uint32(hdr.Entry471.EntrySize)
		if int(off)+57 > len(loaderData) {
			return nil, &ErrLoaderParse{Msg: fmt.Sprintf("471 entry %d offset 0x%x out of range", i, off)}
		}

		entry, err := ParseEntryData(loaderData[off:])
		if err != nil {
			return nil, err
		}
		dev.logf("  471[%d]: %s, offset=0x%x, size=%d, delay=%d",
			i, entry.Name, entry.DataOffset, entry.DataSize, entry.DataDelay)

		if err := downloadSegment(dev, loaderData, entry, CtrlIndex471, rc4); err != nil {
			return nil, fmt.Errorf("471 download: %w", err)
		}

		if entry.DataDelay > 0 {
			time.Sleep(time.Duration(entry.DataDelay) * time.Millisecond)
		}
	}

	// The CRC control transfer blocks until DDR init completes (timeout=0,
	// matching rkdeveloptool). No re-enumeration needed — the boot ROM
	// stays on the same USB connection. Brief delay before 472 (matches
	// rkdeveloptool's 1-second sleep between stages).
	dev.logf("DDR init complete")
	time.Sleep(1 * time.Second)

	// Download 472 entries (USB plug) via control transfers.
	for i := byte(0); i < hdr.Entry472.Count; i++ {
		off := hdr.Entry472.Offset + uint32(i)*uint32(hdr.Entry472.EntrySize)
		if int(off)+57 > len(loaderData) {
			return nil, &ErrLoaderParse{Msg: fmt.Sprintf("472 entry %d offset 0x%x out of range", i, off)}
		}

		entry, err := ParseEntryData(loaderData[off:])
		if err != nil {
			return nil, err
		}
		dev.logf("  472[%d]: %s, offset=0x%x, size=%d",
			i, entry.Name, entry.DataOffset, entry.DataSize)

		if err := downloadSegment(dev, loaderData, entry, CtrlIndex472, rc4); err != nil {
			return nil, fmt.Errorf("472 download: %w", err)
		}

		if entry.DataDelay > 0 {
			time.Sleep(time.Duration(entry.DataDelay) * time.Millisecond)
		}
	}

	dev.logf("USB plug downloaded, waiting for re-enumeration...")

	// Close current device handle — it's about to reset.
	dev.Close()

	// Wait 1 second then poll for re-enumeration (matches rkdeveloptool).
	time.Sleep(1 * time.Second)

	newDev, err := WaitForReconnect(10 * time.Second)
	if err != nil {
		return nil, fmt.Errorf("re-enumeration: %w", err)
	}
	newDev.logger = dev.logger

	// After loader download, the USB plug is running and accepts bulk CBW/CSW.
	// Claim bulk endpoints even though PID still shows maskrom.
	if err := newDev.EnsureBulkEndpoints(); err != nil {
		newDev.Close()
		return nil, fmt.Errorf("claim bulk endpoints after loader: %w", err)
	}

	return newDev, nil
}

// downloadSegment sends a code segment to the device via USB control transfers
// in 4KB chunks with CRC-CCITT appended. Data is sent raw (no RC4 encryption)
// — the boot ROM handles decryption internally based on the BOOT header flags.
func downloadSegment(dev *Device, loader []byte, entry *BootEntryData, wIndex uint16, rc4 bool) error {
	start := entry.DataOffset
	size := entry.DataSize

	if uint32(len(loader)) < start+size {
		return &ErrLoaderParse{
			Msg: fmt.Sprintf("%s: data offset 0x%x + size %d exceeds loader size %d",
				entry.Name, start, size, len(loader)),
		}
	}

	rawData := loader[start : start+size]

	// Copy data for CRC append (don't modify the original loader).
	data := make([]byte, size+5)
	copy(data, rawData)

	// Note: RC4 encryption is NOT applied for the control transfer boot
	// download path. rkdeveloptool sends raw data via control transfers;
	// the boot ROM decrypts internally if needed. RC4 (P_RC4) is only
	// used for the IDB block write path.

	// Adjust size for 4096 alignment edge case.
	totalSize := size
	if totalSize%4096 == 4094 {
		// Need a fill byte — extend by 1 before CRC.
		totalSize++
	}

	// Compute and append CRC-CCITT over the raw data.
	crc := crcCCITT(data[:size])
	data[totalSize] = byte(crc >> 8)     // CRC high byte
	data[totalSize+1] = byte(crc & 0xFF) // CRC low byte
	totalSize += 2

	// Send in 4KB chunks via control transfer.
	var sent uint32
	for sent < totalSize {
		chunkSize := uint32(CtrlChunkSize)
		if remaining := totalSize - sent; remaining < chunkSize {
			chunkSize = remaining
		}

		chunk := data[sent : sent+chunkSize]

		dev.logf("  ctrl: wIndex=0x%04x offset=%d len=%d", wIndex, sent, chunkSize)

		_, err := dev.ControlTransfer(CtrlRequestType, CtrlRequest, CtrlValue, wIndex, chunk)
		if err != nil {
			return fmt.Errorf("control transfer at offset %d: %w", sent, err)
		}

		sent += chunkSize
	}

	return nil
}
