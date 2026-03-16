//go:build windows

package cmd

import (
	"syscall"
	"unsafe"
)

// freeSpace returns available bytes on the filesystem containing path.
func freeSpace(path string) (uint64, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpace := kernel32.NewProc("GetDiskFreeSpaceExW")
	var freeBytes uint64
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	r, _, err := getDiskFreeSpace.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeBytes)),
		0, 0,
	)
	if r == 0 {
		return 0, err
	}
	return freeBytes, nil
}
