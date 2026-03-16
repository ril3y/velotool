package rockusb

/*
#cgo pkg-config: libusb-1.0
#include <libusb.h>
#include <stdlib.h>

// bulk_read performs a synchronous bulk read using libusb_bulk_transfer.
// Returns the number of bytes actually transferred, or a negative libusb error code.
static int bulk_read(libusb_device_handle *dev, unsigned char endpoint,
                     unsigned char *data, int length, unsigned int timeout) {
    int actual = 0;
    int rc = libusb_bulk_transfer(dev, endpoint, data, length, &actual, timeout);
    if (rc == 0 || rc == LIBUSB_ERROR_TIMEOUT) {
        return actual;
    }
    return rc;  // negative error code
}

// bulk_write performs a synchronous bulk write using libusb_bulk_transfer.
static int bulk_write(libusb_device_handle *dev, unsigned char endpoint,
                      unsigned char *data, int length, unsigned int timeout) {
    int actual = 0;
    int rc = libusb_bulk_transfer(dev, endpoint, data, length, &actual, timeout);
    if (rc == 0) {
        return actual;
    }
    return rc;
}

// ctrl_transfer performs a synchronous control transfer with explicit timeout.
// Returns the number of bytes transferred, or a negative libusb error code.
static int ctrl_transfer(libusb_device_handle *dev,
                          uint8_t bmRequestType, uint8_t bRequest,
                          uint16_t wValue, uint16_t wIndex,
                          unsigned char *data, uint16_t wLength,
                          unsigned int timeout) {
    int rc = libusb_control_transfer(dev, bmRequestType, bRequest,
                                      wValue, wIndex, data, wLength, timeout);
    return rc;
}
*/
import "C"

import (
	"fmt"
	"reflect"
	"unsafe"
)

// bulkTimeout is the timeout for bulk transfers in milliseconds.
const bulkTimeout = 10000 // 10 seconds

// getDevHandle extracts the raw libusb_device_handle from a gousb Device.
// This uses unsafe reflection to access gousb's unexported handle field.
func (d *Device) getDevHandle() *C.libusb_device_handle {
	// gousb.Device struct has an unexported 'handle' field of type *libusbDevHandle
	// which is actually *C.libusb_device_handle
	devVal := reflect.ValueOf(d.dev).Elem()
	handleField := devVal.FieldByName("handle")
	if !handleField.IsValid() {
		return nil
	}
	ptr := handleField.Pointer()
	return (*C.libusb_device_handle)(unsafe.Pointer(ptr))
}

// bulkRead performs a synchronous bulk read using libusb_bulk_transfer directly.
// This bypasses gousb's async transfer mechanism which returns 0 bytes on some devices.
func (d *Device) bulkRead(endpoint byte, length int) ([]byte, error) {
	handle := d.getDevHandle()
	if handle == nil {
		return nil, fmt.Errorf("could not get USB device handle")
	}

	buf := C.malloc(C.size_t(length))
	if buf == nil {
		return nil, fmt.Errorf("malloc failed for %d bytes", length)
	}
	defer C.free(buf)

	n := C.bulk_read(handle, C.uchar(endpoint), (*C.uchar)(buf), C.int(length), C.uint(bulkTimeout))
	if n < 0 {
		return nil, fmt.Errorf("libusb_bulk_transfer read: error %d", int(n))
	}

	result := make([]byte, int(n))
	copy(result, C.GoBytes(buf, n))
	return result, nil
}

// controlTransfer performs a synchronous control transfer with explicit timeout.
// Uses libusb directly to avoid gousb's infinite timeout on control transfers.
// Timeout errors are treated as success (data was sent, device is busy executing).
func (d *Device) controlTransfer(bmRequestType, bRequest uint8, wValue, wIndex uint16, data []byte, timeout uint) (int, error) {
	handle := d.getDevHandle()
	if handle == nil {
		return 0, fmt.Errorf("could not get USB device handle")
	}

	var cData unsafe.Pointer
	var wLength C.uint16_t
	if len(data) > 0 {
		cData = C.CBytes(data)
		defer C.free(cData)
		wLength = C.uint16_t(len(data))
	}

	rc := C.ctrl_transfer(handle,
		C.uint8_t(bmRequestType), C.uint8_t(bRequest),
		C.uint16_t(wValue), C.uint16_t(wIndex),
		(*C.uchar)(cData), wLength,
		C.uint(timeout))

	if int(rc) < 0 {
		// LIBUSB_ERROR_TIMEOUT (-7): data was sent but status stage timed out.
		// This is expected when the boot ROM is executing DDR init.
		if int(rc) == -7 {
			return len(data), nil
		}
		return 0, fmt.Errorf("libusb_control_transfer: error %d", int(rc))
	}
	return int(rc), nil
}

// bulkWrite performs a synchronous bulk write using libusb_bulk_transfer directly.
func (d *Device) bulkWrite(endpoint byte, data []byte) error {
	handle := d.getDevHandle()
	if handle == nil {
		return fmt.Errorf("could not get USB device handle")
	}

	cData := C.CBytes(data)
	defer C.free(cData)

	n := C.bulk_write(handle, C.uchar(endpoint), (*C.uchar)(cData), C.int(len(data)), C.uint(bulkTimeout))
	if n < 0 {
		return fmt.Errorf("libusb_bulk_transfer write: error %d", int(n))
	}
	if int(n) != len(data) {
		return fmt.Errorf("short write: %d/%d bytes", int(n), len(data))
	}
	return nil
}
