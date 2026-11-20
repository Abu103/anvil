package elfgen

import (
	"encoding/binary"
	"os"
)

// writeELF writes a minimal ET_EXEC ELF64 file with one RWX PT_LOAD segment.
// The whole file is mapped at baseAddr, with program text and data starting at
// file offset 0x1000 (virtual address textAddr).
func writeELF(path string, image []byte, entry uint64) error {
	const (
		ehSize   = 64
		phSize   = 56
		fileOff  = loadOffset
		pageSize = 0x1000
	)
	out := make([]byte, loadOffset+len(image))
	copy(out, []byte{0x7F, 'E', 'L', 'F', 2, 1, 1, 0})

	binary.LittleEndian.PutUint16(out[16:], 2)      // ET_EXEC
	binary.LittleEndian.PutUint16(out[18:], 0x3E)   // EM_X86_64
	binary.LittleEndian.PutUint32(out[20:], 1)      // EV_CURRENT
	binary.LittleEndian.PutUint64(out[24:], entry)  // e_entry
	binary.LittleEndian.PutUint64(out[32:], ehSize) // e_phoff
	binary.LittleEndian.PutUint64(out[40:], 0)      // e_shoff
	binary.LittleEndian.PutUint32(out[48:], 0)      // e_flags
	binary.LittleEndian.PutUint16(out[52:], ehSize) // e_ehsize
	binary.LittleEndian.PutUint16(out[54:], phSize) // e_phentsize
	binary.LittleEndian.PutUint16(out[56:], 1)      // e_phnum
	binary.LittleEndian.PutUint16(out[58:], 0)      // e_shentsize
	binary.LittleEndian.PutUint16(out[60:], 0)      // e_shnum
	binary.LittleEndian.PutUint16(out[62:], 0)      // e_shstrndx

	ph := out[ehSize : ehSize+phSize]
	binary.LittleEndian.PutUint32(ph[0:], 1)                 // PT_LOAD
	binary.LittleEndian.PutUint32(ph[4:], 7)                 // PF_R|PF_W|PF_X
	binary.LittleEndian.PutUint64(ph[8:], 0)                 // p_offset
	binary.LittleEndian.PutUint64(ph[16:], baseAddr)         // p_vaddr
	binary.LittleEndian.PutUint64(ph[24:], baseAddr)         // p_paddr
	binary.LittleEndian.PutUint64(ph[32:], uint64(len(out))) // p_filesz
	binary.LittleEndian.PutUint64(ph[40:], uint64(len(out))) // p_memsz
	binary.LittleEndian.PutUint64(ph[48:], pageSize)         // p_align

	copy(out[fileOff:], image)
	return os.WriteFile(path, out, 0o755)
}
