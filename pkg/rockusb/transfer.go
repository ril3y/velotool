package rockusb

import (
	"fmt"
	"io"
)

// ProgressFunc is called during transfers to report progress.
// current and total are in bytes.
type ProgressFunc func(current, total int64)

// DefaultMaxRetries is the default number of retries per chunk.
const DefaultMaxRetries = 3

// TransferConfig holds configuration for chunked transfers.
type TransferConfig struct {
	// SectorsPerChunk is the number of sectors per USB transaction.
	// Default: MaxSectorsPerTransfer (128 = 64KB).
	SectorsPerChunk uint16
	// MaxRetries is the number of retries per chunk on failure.
	MaxRetries int
	// OnProgress is called after each successful chunk transfer.
	OnProgress ProgressFunc
}

// DefaultTransferConfig returns the default transfer configuration.
func DefaultTransferConfig() TransferConfig {
	return TransferConfig{
		SectorsPerChunk: MaxSectorsPerTransfer,
		MaxRetries:      DefaultMaxRetries,
	}
}

// ReadToWriter reads sectors from the device and writes to w.
func (d *Device) ReadToWriter(w io.Writer, startLBA uint64, totalSectors uint64, cfg TransferConfig) error {
	if cfg.SectorsPerChunk == 0 {
		cfg.SectorsPerChunk = MaxSectorsPerTransfer
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = DefaultMaxRetries
	}

	totalBytes := int64(totalSectors) * SectorSize
	var bytesRead int64

	lba := startLBA
	remaining := totalSectors

	for remaining > 0 {
		chunkSize := remaining
		if chunkSize > uint64(cfg.SectorsPerChunk) {
			chunkSize = uint64(cfg.SectorsPerChunk)
		}
		chunk := uint16(chunkSize)

		var data []byte
		var err error
		for attempt := 1; attempt <= cfg.MaxRetries; attempt++ {
			data, err = d.ReadLBA(uint32(lba), chunk)
			if err == nil {
				break
			}
			d.logf("Read retry %d/%d at LBA 0x%x: %v", attempt, cfg.MaxRetries, lba, err)
			if attempt == cfg.MaxRetries {
				return &ErrTransfer{
					Op:       "read",
					LBA:      lba,
					Sectors:  chunk,
					Attempt:  attempt,
					MaxRetry: cfg.MaxRetries,
					Err:      err,
				}
			}
		}

		if _, err := w.Write(data); err != nil {
			return fmt.Errorf("write output at LBA 0x%x: %w", lba, err)
		}

		bytesRead += int64(len(data))
		lba += uint64(chunk)
		remaining -= uint64(chunk)

		if cfg.OnProgress != nil {
			cfg.OnProgress(bytesRead, totalBytes)
		}
	}

	return nil
}

// WriteFromReader reads data from r and writes sectors to the device.
func (d *Device) WriteFromReader(r io.Reader, startLBA uint64, totalSectors uint64, cfg TransferConfig) error {
	if cfg.SectorsPerChunk == 0 {
		cfg.SectorsPerChunk = MaxSectorsPerTransfer
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = DefaultMaxRetries
	}

	totalBytes := int64(totalSectors) * SectorSize
	var bytesWritten int64

	lba := startLBA
	remaining := totalSectors

	for remaining > 0 {
		chunkSize := remaining
		if chunkSize > uint64(cfg.SectorsPerChunk) {
			chunkSize = uint64(cfg.SectorsPerChunk)
		}
		chunk := uint16(chunkSize)

		buf := make([]byte, uint32(chunk)*SectorSize)
		n, err := io.ReadFull(r, buf)
		if err != nil && err != io.ErrUnexpectedEOF {
			return fmt.Errorf("read input at LBA 0x%x: %w", lba, err)
		}
		buf = buf[:n]
		// Pad to sector boundary if needed.
		if remainder := n % SectorSize; remainder != 0 {
			padding := make([]byte, SectorSize-remainder)
			buf = append(buf, padding...)
		}
		actualSectors := uint16(len(buf) / SectorSize)

		for attempt := 1; attempt <= cfg.MaxRetries; attempt++ {
			err = d.WriteLBA(uint32(lba), actualSectors, buf)
			if err == nil {
				break
			}
			d.logf("Write retry %d/%d at LBA 0x%x: %v", attempt, cfg.MaxRetries, lba, err)
			if attempt == cfg.MaxRetries {
				return &ErrTransfer{
					Op:       "write",
					LBA:      lba,
					Sectors:  actualSectors,
					Attempt:  attempt,
					MaxRetry: cfg.MaxRetries,
					Err:      err,
				}
			}
		}

		bytesWritten += int64(len(buf))
		lba += uint64(actualSectors)
		remaining -= uint64(actualSectors)

		if cfg.OnProgress != nil {
			cfg.OnProgress(bytesWritten, totalBytes)
		}

		// If we got a short read from input, we're done.
		if n < int(uint32(chunk)*SectorSize) {
			break
		}
	}

	return nil
}
