package gpt

import (
	"encoding/binary"
	"fmt"
	"unicode/utf16"
)

// GPT constants.
const (
	GPTSignature = "EFI PART"
	SectorSize   = 512
	EntrySize    = 128
)

// Header represents a GPT header (LBA 1).
type Header struct {
	Signature      string
	Revision       uint32
	HeaderSize     uint32
	HeaderCRC32    uint32
	MyLBA          uint64
	AlternateLBA   uint64
	FirstUsableLBA uint64
	LastUsableLBA  uint64
	DiskGUID       [16]byte
	PartitionLBA   uint64
	NumEntries     uint32
	EntrySize      uint32
	PartitionCRC32 uint32
}

// Entry represents a single GPT partition entry.
type Entry struct {
	TypeGUID   [16]byte
	UniqueGUID [16]byte
	StartLBA   uint64
	EndLBA     uint64
	Attributes uint64
	Name       string
}

// SizeLBA returns the partition size in sectors.
func (e *Entry) SizeLBA() uint64 {
	if e.EndLBA < e.StartLBA {
		return 0
	}
	return e.EndLBA - e.StartLBA + 1
}

// SizeBytes returns the partition size in bytes.
func (e *Entry) SizeBytes() uint64 {
	return e.SizeLBA() * SectorSize
}

// IsEmpty returns true if this is an unused entry (zero type GUID).
func (e *Entry) IsEmpty() bool {
	for _, b := range e.TypeGUID {
		if b != 0 {
			return false
		}
	}
	return true
}

// Table represents a complete GPT (header + entries).
type Table struct {
	Header  Header
	Entries []Entry
}

// ParseHeader parses a GPT header from raw sector data.
func ParseHeader(data []byte) (*Header, error) {
	if len(data) < 92 {
		return nil, fmt.Errorf("GPT header too short: %d bytes", len(data))
	}

	sig := string(data[0:8])
	if sig != GPTSignature {
		return nil, fmt.Errorf("bad GPT signature: %q", sig)
	}

	h := &Header{
		Signature:      sig,
		Revision:       binary.LittleEndian.Uint32(data[8:12]),
		HeaderSize:     binary.LittleEndian.Uint32(data[12:16]),
		HeaderCRC32:    binary.LittleEndian.Uint32(data[16:20]),
		MyLBA:          binary.LittleEndian.Uint64(data[24:32]),
		AlternateLBA:   binary.LittleEndian.Uint64(data[32:40]),
		FirstUsableLBA: binary.LittleEndian.Uint64(data[40:48]),
		LastUsableLBA:  binary.LittleEndian.Uint64(data[48:56]),
		PartitionLBA:   binary.LittleEndian.Uint64(data[72:80]),
		NumEntries:     binary.LittleEndian.Uint32(data[80:84]),
		EntrySize:      binary.LittleEndian.Uint32(data[84:88]),
		PartitionCRC32: binary.LittleEndian.Uint32(data[88:92]),
	}
	copy(h.DiskGUID[:], data[56:72])

	return h, nil
}

// ParseEntries parses GPT partition entries from raw data.
func ParseEntries(data []byte, numEntries uint32, entrySize uint32) ([]Entry, error) {
	var entries []Entry
	for i := uint32(0); i < numEntries; i++ {
		off := i * entrySize
		if uint32(len(data)) < off+entrySize {
			break
		}
		raw := data[off : off+entrySize]

		e := Entry{
			StartLBA:   binary.LittleEndian.Uint64(raw[32:40]),
			EndLBA:     binary.LittleEndian.Uint64(raw[40:48]),
			Attributes: binary.LittleEndian.Uint64(raw[48:56]),
			Name:       decodeUTF16LE(raw[56:128]),
		}
		copy(e.TypeGUID[:], raw[0:16])
		copy(e.UniqueGUID[:], raw[16:32])

		if e.IsEmpty() {
			continue
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// Parse reads a full GPT table from raw LBA data.
// headerData should be LBA 1 (512 bytes).
// entriesData should be LBAs 2-33 (16KB for 128 entries at 128 bytes each).
func Parse(headerData, entriesData []byte) (*Table, error) {
	h, err := ParseHeader(headerData)
	if err != nil {
		return nil, err
	}

	entries, err := ParseEntries(entriesData, h.NumEntries, h.EntrySize)
	if err != nil {
		return nil, err
	}

	return &Table{Header: *h, Entries: entries}, nil
}

// decodeUTF16LE decodes a null-terminated UTF-16LE string.
func decodeUTF16LE(data []byte) string {
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}
	u16 := make([]uint16, len(data)/2)
	for i := range u16 {
		u16[i] = binary.LittleEndian.Uint16(data[i*2 : i*2+2])
	}
	// Trim null terminators.
	for len(u16) > 0 && u16[len(u16)-1] == 0 {
		u16 = u16[:len(u16)-1]
	}
	return string(utf16.Decode(u16))
}
