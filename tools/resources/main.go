//go:build ignore

// resourcegen writes an AMD64 COFF resource object consumed by the Go linker.
// It avoids requiring an installed C compiler or a resource-compiler toolchain.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"os"
)

const manifest = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
 <assemblyIdentity version="1.0.0.0" processorArchitecture="amd64" name="InternetStability.Client" type="win32"/>
 <description>Internet Stability portable client</description>
 <dependency><dependentAssembly><assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="amd64" publicKeyToken="6595b64144ccf1df" language="*"/></dependentAssembly></dependency>
 <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3"><security><requestedPrivileges><requestedExecutionLevel level="asInvoker" uiAccess="false"/></requestedPrivileges></security></trustInfo>
 <application xmlns="urn:schemas-microsoft-com:asm.v3"><windowsSettings><dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true</dpiAware><longPathAware xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">true</longPathAware></windowsSettings></application>
 <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1"><application><supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/></application></compatibility>
</assembly>`

func main() {
	output := flag.String("output", "data/build/client_windows_amd64.syso", "COFF resource output")
	flag.Parse()

	resource := manifestResource()
	object := resourceObject(resource)
	if err := os.WriteFile(*output, object, 0600); err != nil {
		panic(err)
	}
}

func manifestResource() []byte {
	const resourceDataOffset = 88
	resource := make([]byte, (resourceDataOffset+len(manifest)+3)&^3)
	u16 := func(at int, value uint16) {
		binary.LittleEndian.PutUint16(resource[at:], value)
	}
	u32 := func(at int, value uint32) {
		binary.LittleEndian.PutUint32(resource[at:], value)
	}

	// Type 24 (RT_MANIFEST), resource ID 1, language en-US. All directory offsets
	// are relative to the beginning of the resource section.
	u16(14, 1)
	u32(16, 24)
	u32(20, 0x80000000|24)
	u16(24+14, 1)
	u32(40, 1)
	u32(44, 0x80000000|48)
	u16(48+14, 1)
	u32(64, 1033)
	u32(68, 72)
	u32(72, resourceDataOffset)
	u32(76, uint32(len(manifest)))
	copy(resource[resourceDataOffset:], manifest)

	return resource
}

func resourceObject(resource []byte) []byte {
	var out bytes.Buffer
	write := func(value any) {
		if err := binary.Write(&out, binary.LittleEndian, value); err != nil {
			panic(err)
		}
	}

	const dataPointer = 60
	relocPointer := uint32(dataPointer + len(resource))
	symbolPointer := relocPointer + 10
	// IMAGE_FILE_HEADER.
	write(uint16(0x8664))
	write(uint16(1))
	write(uint32(0))
	write(symbolPointer)
	write(uint32(1))
	write(uint16(0))
	write(uint16(0))
	// IMAGE_SECTION_HEADER.
	write([8]byte{'.', 'r', 's', 'r', 'c'})
	write(uint32(0))
	write(uint32(0))
	write(uint32(len(resource)))
	write(uint32(dataPointer))
	write(relocPointer)
	write(uint32(0))
	write(uint16(1))
	write(uint16(0))
	write(uint32(0x40000040))
	out.Write(resource)
	// Relocate the resource data RVA against the .rsrc section symbol.
	write(uint32(72))
	write(uint32(0))
	write(uint16(0x0003))
	write([8]byte{'.', 'r', 's', 'r', 'c'})
	write(uint32(0))
	write(int16(1))
	write(uint16(0))
	write(uint8(3))
	write(uint8(0))
	write(uint32(4)) // Empty COFF string table.
	return out.Bytes()
}
