//go:build windows && amd64

package collectors

import "unsafe"

// Assert SDK layout at compile time as well as in Windows runtime tests. Both
// subtractions must be nonnegative; a changed Go/native layout fails crossbuild.
var (
	_ [1352 - unsafe.Sizeof(mibIfRow2{})]byte
	_ [unsafe.Sizeof(mibIfRow2{}) - 1352]byte
	_ [1152 - unsafe.Offsetof(mibIfRow2{}.Flags)]byte
	_ [unsafe.Offsetof(mibIfRow2{}.Flags) - 1152]byte
	_ [1192 - unsafe.Offsetof(mibIfRow2{}.TransmitLinkSpeed)]byte
	_ [unsafe.Offsetof(mibIfRow2{}.TransmitLinkSpeed) - 1192]byte
	_ [168 - unsafe.Sizeof(mibIPInterfaceRow{})]byte
	_ [unsafe.Sizeof(mibIPInterfaceRow{}) - 168]byte
	_ [42 - unsafe.Offsetof(mibIPInterfaceRow{}.WeakHostSend)]byte
	_ [unsafe.Offsetof(mibIPInterfaceRow{}.WeakHostSend) - 42]byte
	_ [208 - unsafe.Offsetof(ipAdapterAddresses{}.FirstGateway)]byte
	_ [unsafe.Offsetof(ipAdapterAddresses{}.FirstGateway) - 208]byte
	_ [1240 - unsafe.Offsetof(mibIfRow2{}.InErrors)]byte
	_ [unsafe.Offsetof(mibIfRow2{}.InErrors) - 1240]byte
	_ [16 - unsafe.Sizeof(nativeSocketAddress{})]byte
	_ [unsafe.Sizeof(nativeSocketAddress{}) - 16]byte
	_ [112 - unsafe.Sizeof(captureShellExecuteInfo{})]byte
	_ [unsafe.Sizeof(captureShellExecuteInfo{}) - 112]byte
	_ [104 - unsafe.Offsetof(captureShellExecuteInfo{}.Process)]byte
	_ [unsafe.Offsetof(captureShellExecuteInfo{}.Process) - 104]byte
	_ [568 - unsafe.Sizeof(captureProcessEntry{})]byte
	_ [unsafe.Sizeof(captureProcessEntry{}) - 568]byte
	_ [44 - unsafe.Offsetof(captureProcessEntry{}.ExeFile)]byte
	_ [unsafe.Offsetof(captureProcessEntry{}.ExeFile) - 44]byte
	_ [72 - unsafe.Sizeof(captureMemoryCounters{})]byte
	_ [unsafe.Sizeof(captureMemoryCounters{}) - 72]byte
)
