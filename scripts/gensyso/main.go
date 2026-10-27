//go:build ignore

// Command gensyso writes the Windows resource object that gives the
// executable its icon.
//
// Windows keeps the icon of a program in the resource section of the
// executable. The section is built here from the icon file and written as a
// COFF object file; the Go linker adds such a file to the binary, so the icon
// ends up in the executable without any extra tool.
//
// The name of the object file carries the target it was built for, so that it
// is only used on the matching platform.
//
// Usage:
//
//	go run scripts/gensyso/main.go
package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// The resource types of the icon.
const (
	rtIcon      = 3
	rtGroupIcon = 14
)

// langEnglish is the language of the resources; the program has no text, so
// any language will do.
const langEnglish = 0x0409

// The sizes of the parts of a resource directory.
const (
	dirHeaderSize = 16
	dirEntrySize  = 8
	dataEntrySize = 16
)

// The machine types of the object files.
const (
	machineAMD64 = 0x8664
	machineARM64 = 0xaa64
)

// sectionCharacteristics says that the section holds initialised data that
// can be read.
const sectionCharacteristics = 0x40000040

// The relocation types that say "the address of this field".
const (
	relAMD64Addr32 = 0x0002
	relARM64Addr32 = 0x0001
)

// icoImage is one image of an icon file.
type icoImage struct {
	width  int
	height int
	planes int
	bits   int
	data   []byte
}

// reloc is a relocation of the resource section.
type reloc struct {
	offset uint32 // the offset of the field inside the section
}

// target is one object file to write.
type target struct {
	name    string
	machine uint16
}

func main() {
	root := repoRoot()
	ico, err := os.ReadFile(filepath.Join(root, "internal", "assets", "icon.ico"))
	if err != nil {
		fail(err)
	}
	images, err := parseICO(ico)
	if err != nil {
		fail(err)
	}

	section, relocs, err := buildResource(images)
	if err != nil {
		fail(err)
	}

	targets := []target{
		{"rsrc_windows_amd64.syso", machineAMD64},
		{"rsrc_windows_arm64.syso", machineARM64},
	}
	dir := filepath.Join(root, "cmd", "ssh-tunnel")
	for _, t := range targets {
		kind := uint16(relAMD64Addr32)
		if t.machine == machineARM64 {
			kind = relARM64Addr32
		}
		obj := buildCOFF(t.machine, section, relocs, kind)
		path := filepath.Join(dir, t.name)
		if err := os.WriteFile(path, obj, 0o644); err != nil {
			fail(err)
		}
		fmt.Println("wrote", path)
	}
}

// parseICO reads the images of an icon file.
func parseICO(data []byte) ([]icoImage, error) {
	if len(data) < 6 {
		return nil, errors.New("the icon file is too short")
	}
	if binary.LittleEndian.Uint16(data[0:2]) != 0 {
		return nil, errors.New("the file is not an icon")
	}
	if binary.LittleEndian.Uint16(data[2:4]) != 1 {
		return nil, errors.New("the file is not an icon")
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	images := make([]icoImage, 0, count)
	for i := 0; i < count; i++ {
		entry := data[6+16*i:]
		if len(entry) < 16 {
			return nil, errors.New("the icon file is truncated")
		}
		size := int(binary.LittleEndian.Uint32(entry[8:12]))
		offset := int(binary.LittleEndian.Uint32(entry[12:16]))
		if offset+size > len(data) {
			return nil, errors.New("the icon file is truncated")
		}
		images = append(images, icoImage{
			// A zero width or height means 256 pixels.
			width:  int(entry[0]),
			height: int(entry[1]),
			planes: int(binary.LittleEndian.Uint16(entry[4:6])),
			bits:   int(binary.LittleEndian.Uint16(entry[6:8])),
			data:   data[offset : offset+size],
		})
	}
	if len(images) == 0 {
		return nil, errors.New("the icon file has no image")
	}
	return images, nil
}

// buildResource builds the resource section of the icon and the relocations
// that tell the linker where every image ended up.
func buildResource(images []icoImage) ([]byte, []reloc, error) {
	n := len(images)

	// The layout of the section:
	//
	//	types        the directory of the resource types
	//	icon names   the names of the icon images
	//	group names  the name of the group of icons
	//	languages    one language directory per name
	//	data entries one entry per image and one for the group
	//	images       the images themselves
	typesOffset := 0
	iconNamesOffset := typesOffset + dirHeaderSize + 2*dirEntrySize
	groupNamesOffset := iconNamesOffset + dirHeaderSize + n*dirEntrySize
	languagesOffset := groupNamesOffset + dirHeaderSize + dirEntrySize
	entriesOffset := languagesOffset + (n+1)*(dirHeaderSize+dirEntrySize)
	dataOffset := entriesOffset + (n+1)*dataEntrySize

	// Every image comes first, then the group that ties them together; the
	// order matches the order of the data entries.
	blobs := make([][]byte, 0, n+1)
	for _, img := range images {
		blobs = append(blobs, img.data)
	}
	blobs = append(blobs, groupIcon(images))

	// The images are aligned so that the linker can copy them easily.
	data := make([]byte, 0, dataOffset+len(blobs)*64)
	offsets := make([]int, 0, len(blobs))
	for _, blob := range blobs {
		for len(data)%8 != 0 {
			data = append(data, 0)
		}
		offsets = append(offsets, dataOffset+len(data))
		data = append(data, blob...)
	}

	buf := &bytes.Buffer{}
	write := func(v any) { binary.Write(buf, binary.LittleEndian, v) }

	// The directory of the types.
	write(uint32(0)) // characteristics
	write(uint32(0)) // time stamp
	write(uint16(0)) // major version
	write(uint16(0)) // minor version
	write(uint16(0)) // named entries
	write(uint16(2)) // id entries
	write(uint32(rtIcon))
	write(uint32(0x80000000 | uint32(iconNamesOffset)))
	write(uint32(rtGroupIcon))
	write(uint32(0x80000000 | uint32(groupNamesOffset)))

	// The names of the icon images.
	write(uint32(0))
	write(uint32(0))
	write(uint16(0))
	write(uint16(0))
	write(uint16(0))
	write(uint16(uint16(n)))
	for i := 0; i < n; i++ {
		write(uint32(i + 1))
		write(uint32(0x80000000 | uint32(languagesOffset+i*(dirHeaderSize+dirEntrySize))))
	}

	// The name of the group of icons.
	write(uint32(0))
	write(uint32(0))
	write(uint16(0))
	write(uint16(0))
	write(uint16(0))
	write(uint16(1))
	write(uint32(1))
	write(uint32(0x80000000 | uint32(languagesOffset+n*(dirHeaderSize+dirEntrySize))))

	// The language of every name.
	for i := 0; i <= n; i++ {
		write(uint32(0))
		write(uint32(0))
		write(uint16(0))
		write(uint16(0))
		write(uint16(0))
		write(uint16(1))
		write(uint32(langEnglish))
		write(uint32(entriesOffset + i*dataEntrySize))
	}

	// The entries of the data. The linker turns their offset into the
	// address of the image.
	for i, blob := range blobs {
		write(uint32(offsets[i]))
		write(uint32(len(blob)))
		write(uint32(0)) // code page
		write(uint32(0)) // reserved
	}

	section := make([]byte, dataOffset+len(data))
	copy(section, buf.Bytes())
	copy(section[dataOffset:], data)

	// Every entry needs a relocation so that the linker fills in the
	// address of the image.
	relocs := make([]reloc, 0, len(blobs))
	for i := range blobs {
		relocs = append(relocs, reloc{offset: uint32(entriesOffset + i*dataEntrySize)})
	}
	return section, relocs, nil
}

// groupIcon builds the directory that groups the images of the icon.
func groupIcon(images []icoImage) []byte {
	buf := &bytes.Buffer{}
	binary.Write(buf, binary.LittleEndian, uint16(0)) // reserved
	binary.Write(buf, binary.LittleEndian, uint16(1)) // type: icon
	binary.Write(buf, binary.LittleEndian, uint16(len(images)))
	for i, img := range images {
		width, height := img.width, img.height
		if width == 0 {
			width = 256
		}
		if height == 0 {
			height = 256
		}
		buf.WriteByte(byte(width))
		buf.WriteByte(byte(height))
		buf.WriteByte(0) // colours
		buf.WriteByte(0) // reserved
		binary.Write(buf, binary.LittleEndian, uint16(img.planes))
		binary.Write(buf, binary.LittleEndian, uint16(img.bits))
		binary.Write(buf, binary.LittleEndian, uint32(len(img.data)))
		binary.Write(buf, binary.LittleEndian, uint16(i+1))
	}
	return buf.Bytes()
}

// buildCOFF writes the resource section as a COFF object file with a single
// section.
func buildCOFF(machine uint16, section []byte, relocs []reloc, kind uint16) []byte {
	const (
		headerSize    = 20
		sectionHeader = 40
		symbolSize    = 18
		relocSize     = 10
	)

	dataOffset := headerSize + sectionHeader
	symbolsOffset := dataOffset + len(section)
	// The string table holds the names that are too long for the symbol
	// table; it is empty but still has to be there.
	stringOffset := symbolsOffset + symbolSize
	relocsOffset := stringOffset + 4

	buf := &bytes.Buffer{}
	write := func(v any) { binary.Write(buf, binary.LittleEndian, v) }

	// The header of the file.
	write(machine)
	write(uint16(1)) // number of sections
	write(uint32(0)) // time stamp
	write(uint32(symbolsOffset))
	write(uint32(1)) // number of symbols
	write(uint16(0)) // size of the optional header
	write(uint16(0)) // characteristics

	// The header of the section.
	buf.WriteString(".rsrc")
	buf.Write([]byte{0, 0, 0})
	write(uint32(len(section))) // virtual size
	write(uint32(0))            // virtual address
	write(uint32(len(section))) // size of the raw data
	write(uint32(dataOffset))
	write(uint32(relocsOffset))
	write(uint32(0)) // line numbers
	write(uint16(len(relocs)))
	write(uint16(0)) // line numbers
	write(uint32(sectionCharacteristics))

	// The section itself.
	buf.Write(section)

	// The symbol of the section.
	buf.WriteString(".rsrc")
	buf.Write([]byte{0, 0, 0})
	write(uint32(0)) // value
	write(uint16(1)) // section number
	write(uint16(0)) // type
	write(uint8(3))  // storage class: static
	write(uint8(0))  // auxiliary symbols

	// The string table: only its size, which counts itself.
	write(uint32(4))

	// The relocations that fix the addresses of the images.
	for _, r := range relocs {
		write(r.offset)
		write(uint32(0)) // the symbol of the section
		write(kind)
	}

	return buf.Bytes()
}

// repoRoot returns the root of the repository, derived from the path of the
// source file.
func repoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		fail(errors.New("cannot locate the source file"))
	}
	// The file lives in scripts/gensyso.
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
