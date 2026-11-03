//go:build windows

package collectors

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

var wlanAPI = syscall.NewLazyDLL("wlanapi.dll")

var wlanOpenHandle = wlanAPI.NewProc("WlanOpenHandle")

var wlanCloseHandle = wlanAPI.NewProc("WlanCloseHandle")

var wlanQueryInterface = wlanAPI.NewProc("WlanQueryInterface")

var wlanFreeMemory = wlanAPI.NewProc("WlanFreeMemory")

var clsidFromString = syscall.NewLazyDLL("ole32.dll").NewProc("CLSIDFromString")

func parseNativeGUID(text string) (nativeGUID, error) {
	var guid nativeGUID
	p, err := syscall.UTF16PtrFromString("{" + text + "}")
	if err != nil {
		return guid, err
	}

	result, _, _ := clsidFromString.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&guid)))
	if int32(result) < 0 {
		return guid, fmt.Errorf("invalid interface GUID: %s", text)
	}

	return guid, nil
}

func queryWLAN(handle uintptr, guid *nativeGUID, opcode uint32) ([]byte, uint32) {
	var size uint32
	var data unsafe.Pointer
	result, _, _ := wlanQueryInterface.Call(handle, uintptr(unsafe.Pointer(guid)), uintptr(opcode), 0, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&data)), 0)
	if data != nil {
		defer wlanFreeMemory.Call(uintptr(data))
	}

	if result != 0 {
		return nil, uint32(result)
	}

	if size > 1<<20 || (size > 0 && data == nil) {
		return nil, 13
	}

	return append([]byte(nil), unsafe.Slice((*byte)(data), int(size))...), 0
}

func (c *nativeCollector) Radio(ctx context.Context, adapter Adapter) (RadioSample, error) {
	sample := RadioSample{
		Time:          time.Now().Format(time.RFC3339Nano),
		InterfaceGUID: adapter.InterfaceGUID,
		Phys:          []PHYStats{},
		Raw:           make(map[string]any),
	}
	if err := ctx.Err(); err != nil {
		return sample, err
	}

	if adapter.Kind != "wifi" {
		return sample, errors.New("radio statistics require a Wi-Fi interface")
	}

	if err := wlanAPI.Load(); err != nil {
		return sample, fmt.Errorf("Native Wi-Fi API unavailable: %w", err)
	}

	guid, err := parseNativeGUID(adapter.InterfaceGUID)
	if err != nil {
		return sample, err
	}

	var version uint32
	var handle uintptr
	result, _, _ := wlanOpenHandle.Call(2, 0, uintptr(unsafe.Pointer(&version)), uintptr(unsafe.Pointer(&handle)))
	sample.Raw["openError"] = uint32(result)
	sample.Raw["time"] = sample.Time
	if result != 0 {
		sample.StatisticsError = uint32(result)

		return sample, fmt.Errorf("WlanOpenHandle: %w", syscall.Errno(result))
	}

	defer wlanCloseHandle.Call(handle, 0)
	data, code := queryWLAN(handle, &guid, 0x10000101)
	sample.StatisticsError = code
	statistics := map[string]any{"error": code, "bytes": len(data)}
	if code == 0 {
		sample.Phys, err = ParseWLANStatistics(data)
		if err != nil {
			sample.Error = err.Error()
			statistics["parseError"] = err.Error()
		} else {
			sample.StatisticsAvailable = len(sample.Phys) > 0
			sample.DuplicatePHYCounters = DuplicatePHYCounters(sample.Phys)
			statistics["phys"] = sample.Phys
			statistics["numberPhys"] = len(sample.Phys)
			statistics["unicastTxFrames"] = binary.LittleEndian.Uint64(data[24:32])
			statistics["unicastRxFrames"] = binary.LittleEndian.Uint64(data[32:40])
			statistics["unicastDecryptFailures"] = binary.LittleEndian.Uint64(data[112:120])
		}
	}

	sample.Raw["statistics"] = statistics
	data, code = queryWLAN(handle, &guid, 0x10000102)
	rssi := map[string]any{"error": code, "bytes": len(data)}
	if code == 0 && len(data) >= 4 {
		value := int32(binary.LittleEndian.Uint32(data))
		sample.RSSIDbm = &value
		rssi["rssiDbm"] = value
	}

	sample.Raw["rssi"] = rssi
	data, code = queryWLAN(handle, &guid, 19)
	quality := map[string]any{"error": code, "bytes": len(data)}
	if code == 0 && len(data) >= 24 {
		value, rx, tx := binary.LittleEndian.Uint32(data[4:8]), binary.LittleEndian.Uint32(data[8:12]), binary.LittleEndian.Uint32(data[12:16])
		sample.QualityPct = &value
		sample.RxRateNative = &rx
		sample.TxRateNative = &tx
		quality["phyType"] = binary.LittleEndian.Uint32(data[0:4])
		quality["linkQualityPct"] = value
		quality["rxRate"] = rx
		quality["txRate"] = tx
		quality["mlo"] = binary.LittleEndian.Uint32(data[16:20])
		quality["numberLinks"] = binary.LittleEndian.Uint32(data[20:24])
		quality["rawBase64"] = base64.StdEncoding.EncodeToString(data)
	}
	// Keep unsupported realtime quality unavailable. Querying current_connection
	// would also retrieve SSID/BSSID-bearing data and expand the privacy boundary.
	sample.Raw["quality"] = quality
	// Unsupported driver statistics are data unavailability, not zero counters.
	return sample, nil
}
