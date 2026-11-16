//go:build darwin && cgo

package telemetry

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <stdlib.h>
#include <IOKit/IOKitLib.h>
#include <CoreFoundation/CoreFoundation.h>

// platformUUID copies the IOPlatformUUID of the IOPlatformExpertDevice,
// the line `ioreg -rd1 -c IOPlatformExpertDevice` prints, into buf.
static int platformUUID(char *buf, int size) {
	io_service_t dev = IOServiceGetMatchingService(MACH_PORT_NULL, IOServiceMatching("IOPlatformExpertDevice"));
	if (dev == IO_OBJECT_NULL) {
		return 0;
	}
	CFTypeRef uuid = IORegistryEntryCreateCFProperty(dev, CFSTR("IOPlatformUUID"), kCFAllocatorDefault, 0);
	IOObjectRelease(dev);
	if (uuid == NULL) {
		return 0;
	}
	int ok = CFGetTypeID(uuid) == CFStringGetTypeID() &&
		CFStringGetCString((CFStringRef)uuid, buf, size, kCFStringEncodingUTF8);
	CFRelease(uuid);
	return ok;
}
*/
import "C"

import (
	"errors"
	"strings"
	"unsafe"
)

// systemMachineID reads the IOPlatformUUID through IOKit, in the app,
// rather than starting ioreg as denisbrodbeck/machineid does: the same
// value, which security software does not take for system discovery.
func systemMachineID() (string, error) {
	buf := (*C.char)(C.malloc(256))
	defer C.free(unsafe.Pointer(buf))
	if C.platformUUID(buf, 256) == 0 {
		return "", errors.New("no IOPlatformUUID")
	}
	return strings.TrimSpace(C.GoString(buf)), nil
}
