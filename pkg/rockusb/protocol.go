package rockusb

import (
	"encoding/binary"
	"fmt"
)

// USB bulk transfer constants.
const (
	CBWSignature = 0x43425355 // "USBC"
	CSWSignature = 0x53425355 // "USBS"
	CBWSize      = 31
	CSWSize      = 13

	DirectionOut = 0x00
	DirectionIn  = 0x80

	// Sector size for LBA addressing.
	SectorSize = 512

	// Maximum sectors per USB transaction (128 sectors = 64KB).
	MaxSectorsPerTransfer = 128

	// CDB length used by rkdeveloptool.
	CDBLength = 0x0a
)

// Rockchip USB opcodes.
// CDB format: byte 0 = opcode, bytes 1-4 = address (BE), bytes 5-6 = count (BE).
// No direction byte in CDB — direction is only in CBW flags (byte 12).
const (
	OpTestUnitReady = 0x00
	OpReadFlashID   = 0x01
	OpTestBadBlock  = 0x03
	OpReadSector    = 0x04
	OpWriteSector   = 0x05
	OpEraseNormal   = 0x06
	OpReadLBA       = 0x14
	OpWriteLBA      = 0x15
	OpReadSDRAM     = 0x17
	OpWriteSDRAM    = 0x18
	OpExecuteSDRAM  = 0x19
	OpReadFlashInfo = 0x1A
	OpReadChipInfo  = 0x1B
	OpReadCapability = 0x1D
	OpResetDevice   = 0xFF
)

// CBW represents a USB Command Block Wrapper.
type CBW struct {
	Signature  uint32
	Tag        uint32
	DataLength uint32
	Flags      byte
	LUN        byte
	CDBLength  byte
	CDB        [16]byte
}

// Encode serializes a CBW to bytes.
func (c *CBW) Encode() []byte {
	buf := make([]byte, CBWSize)
	binary.LittleEndian.PutUint32(buf[0:4], c.Signature)
	binary.LittleEndian.PutUint32(buf[4:8], c.Tag)
	binary.LittleEndian.PutUint32(buf[8:12], c.DataLength)
	buf[12] = c.Flags
	buf[13] = c.LUN
	buf[14] = c.CDBLength
	copy(buf[15:31], c.CDB[:])
	return buf
}

// CSW represents a USB Command Status Wrapper.
type CSW struct {
	Signature uint32
	Tag       uint32
	Residue   uint32
	Status    byte
}

// DecodeCSW parses a CSW from bytes.
func DecodeCSW(buf []byte) (*CSW, error) {
	if len(buf) < CSWSize {
		return nil, fmt.Errorf("CSW too short: %d bytes", len(buf))
	}
	csw := &CSW{
		Signature: binary.LittleEndian.Uint32(buf[0:4]),
		Tag:       binary.LittleEndian.Uint32(buf[4:8]),
		Residue:   binary.LittleEndian.Uint32(buf[8:12]),
		Status:    buf[12],
	}
	if csw.Signature != CSWSignature {
		return nil, fmt.Errorf("bad CSW signature: 0x%08x", csw.Signature)
	}
	return csw, nil
}

// NewCBW creates a CBW with the standard signature.
func NewCBW(tag uint32, dataLen uint32, direction byte, cdb [16]byte) *CBW {
	return &CBW{
		Signature:  CBWSignature,
		Tag:        tag,
		DataLength: dataLen,
		Flags:      direction,
		LUN:        0,
		CDBLength:  CDBLength,
		CDB:        cdb,
	}
}

// CDB builders — rkdeveloptool CBWCB format:
//   CDB[0]   = opcode
//   CDB[1]   = subcode (reserved, 0x00)
//   CDB[2-5] = address (big-endian uint32)
//   CDB[6]   = reserved (0x00)
//   CDB[7-8] = length/count (big-endian uint16)
//   CDB[9-15] = reserved

// BuildReadLBACDB creates a CDB for reading LBA sectors.
func BuildReadLBACDB(lba uint32, count uint16) [16]byte {
	var cdb [16]byte
	cdb[0] = OpReadLBA
	binary.BigEndian.PutUint32(cdb[2:6], lba)
	binary.BigEndian.PutUint16(cdb[7:9], count)
	return cdb
}

// BuildWriteLBACDB creates a CDB for writing LBA sectors.
func BuildWriteLBACDB(lba uint32, count uint16) [16]byte {
	var cdb [16]byte
	cdb[0] = OpWriteLBA
	binary.BigEndian.PutUint32(cdb[2:6], lba)
	binary.BigEndian.PutUint16(cdb[7:9], count)
	return cdb
}

// BuildResetCDB creates a CDB for resetting the device.
func BuildResetCDB(subcode byte) [16]byte {
	var cdb [16]byte
	cdb[0] = OpResetDevice
	cdb[1] = subcode
	return cdb
}

// BuildReadChipInfoCDB creates a CDB for reading chip info.
func BuildReadChipInfoCDB() [16]byte {
	var cdb [16]byte
	cdb[0] = OpReadChipInfo
	return cdb
}

// BuildReadFlashInfoCDB creates a CDB for reading flash info.
func BuildReadFlashInfoCDB() [16]byte {
	var cdb [16]byte
	cdb[0] = OpReadFlashInfo
	return cdb
}

// BuildTestUnitReadyCDB creates a CDB for testing if the device is ready.
func BuildTestUnitReadyCDB() [16]byte {
	var cdb [16]byte
	cdb[0] = OpTestUnitReady
	return cdb
}

// BuildReadFlashIDCDB creates a CDB for reading the flash ID.
func BuildReadFlashIDCDB() [16]byte {
	var cdb [16]byte
	cdb[0] = OpReadFlashID
	return cdb
}
