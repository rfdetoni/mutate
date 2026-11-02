package elfanalyzer

import (
	"bytes"
	"debug/elf"
	"fmt"
	"io"
	"os"
	"path/filepath"
 "sort"
	"strings"

	"github.com/rfdetoni/mutate/internal/domain"
)

var elfMagic = []byte{0x7f, 'E', 'L', 'F'}

// IsELF checks if a file begins with standard ELF magic bytes.
func IsELF(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 4)
	if _, err := io.ReadFull(f, buf); err != nil {
		return false
	}
	return bytes.Equal(buf, elfMagic)
}

// AnalyzeELF parses an ELF file and populates an ElfInfo struct.
func AnalyzeELF(path string) (*domain.ElfInfo, error) {
	f, err := elf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open elf %s: %w", path, err)
	}
	defer f.Close()

	info := &domain.ElfInfo{
		Architecture: f.Machine.String(),
	}

	// 1. Interpreter (PT_INTERP)
	for _, prog := range f.Progs {
		if prog.Type == elf.PT_INTERP {
			interpBytes := make([]byte, prog.Filesz)
			if _, err := prog.ReadAt(interpBytes, 0); err == nil {
				// C-string null termination trim
				info.Interpreter = strings.TrimRight(string(interpBytes), "\x00")
				if strings.Contains(info.Interpreter, "musl") {
					info.NeedsMusl = true
				}
				if strings.Contains(info.Interpreter, "ld-linux") {
					info.NeedsGlibc = true
				}
			}
			break
		}
	}

	// 2. DT_NEEDED libraries
	libs, err := f.ImportedLibraries()
	if err == nil {
		info.DynamicLibraries = libs
	}

	// 3. RPATH & RUNPATH
	if rpaths, err := f.DynString(elf.DT_RPATH); err == nil {
		info.RPath = rpaths
	}
	if runpaths, err := f.DynString(elf.DT_RUNPATH); err == nil {
		info.RunPath = runpaths
	}

	// 4. SONAME
	if sonames, err := f.DynString(elf.DT_SONAME); err == nil && len(sonames) > 0 {
		info.Soname = sonames[0]
	}

	return info, nil
}

// PackageAnalysisResult holds results of analyzing all ELFs in a package.
type PackageAnalysisResult struct {
	TotalELFs         int
	InternalLibraries map[string]string // soname/basename -> relative path inside package
	ExternalLibraries []string          // Deduplicated list of needed .so NOT satisfied internally
	ElfDetails        map[string]*domain.ElfInfo
	HasMusl           bool
	HasGlibc          bool
}

// AnalyzePackage scans staging files, indexes internal .so files, and finds missing external libraries.
func AnalyzePackage(ir *domain.PackageIR) (*PackageAnalysisResult, error) {
	result := &PackageAnalysisResult{
		InternalLibraries: make(map[string]string),
		ElfDetails:        make(map[string]*domain.ElfInfo),
	}

	// Step 1: Catalog internal libraries (.so files) provided by the package
	for destPath := range ir.Files {
		base := filepath.Base(destPath)
		if strings.Contains(base, ".so") {
			result.InternalLibraries[base] = destPath
		}
	}

	// Step 2: Scan files for ELF binaries
	neededSet := make(map[string]struct{})

	for destPath, entry := range ir.Files {
		if entry.IsDir || entry.IsSymlink {
			continue
		}

		filePath := entry.SourcePath
		if filePath == "" && ir.StagingDir != "" {
			filePath = filepath.Join(ir.StagingDir, destPath)
		}

		if !IsELF(filePath) {
			continue
		}

		elfInfo, err := AnalyzeELF(filePath)
		if err != nil {
			return nil,fmt.Errorf("invalid ELF %s: %w",destPath,err)
		}

		result.TotalELFs++
		result.ElfDetails[destPath] = elfInfo
		if elfInfo.Soname != "" {
			result.InternalLibraries[elfInfo.Soname] = destPath
		}
		if elfInfo.NeedsMusl {
			result.HasMusl = true
		}
		if elfInfo.NeedsGlibc {
			result.HasGlibc = true
		}

		for _, lib := range elfInfo.DynamicLibraries {
			neededSet[lib] = struct{}{}
		}

		// Update entry in IR
		entry.Elf = elfInfo
		ir.Files[destPath] = entry
	}

	// Step 3: Filter out internal libraries from needed external libraries
	for lib := range neededSet {
		if _, internal := result.InternalLibraries[lib]; !internal {
			result.ExternalLibraries = append(result.ExternalLibraries, lib)
		}
	}

 sort.Strings(result.ExternalLibraries)
 return result,nil
}
