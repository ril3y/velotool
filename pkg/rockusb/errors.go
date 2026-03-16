package rockusb

import "fmt"

// ErrDeviceNotFound is returned when no Rockchip device is found on USB.
var ErrDeviceNotFound = fmt.Errorf("no Rockchip device found (VID 0x2207)")

// ErrCSWFailed is returned when the device reports a command failure.
type ErrCSWFailed struct {
	Tag    uint32
	Status byte
}

func (e *ErrCSWFailed) Error() string {
	return fmt.Sprintf("CSW status %d for tag 0x%08x", e.Status, e.Tag)
}

// ErrTransfer is returned when a read/write transfer fails.
type ErrTransfer struct {
	Op       string // "read" or "write"
	LBA      uint64
	Sectors  uint16
	Attempt  int
	MaxRetry int
	Err      error
}

func (e *ErrTransfer) Error() string {
	return fmt.Sprintf("%s at LBA 0x%x (%d sectors) failed on attempt %d/%d: %v",
		e.Op, e.LBA, e.Sectors, e.Attempt, e.MaxRetry, e.Err)
}

func (e *ErrTransfer) Unwrap() error { return e.Err }

// ErrLoaderParse is returned when the loader binary cannot be parsed.
type ErrLoaderParse struct {
	Msg string
}

func (e *ErrLoaderParse) Error() string {
	return fmt.Sprintf("loader parse error: %s", e.Msg)
}
