package domain

import (
	"io/fs"
	"time"
)

// Format represents package format type.
type Format string

const (
	FormatUnknown    Format = "unknown"
	FormatRPM        Format = "rpm"
	FormatDEB        Format = "deb"
	FormatArch       Format = "arch"
	FormatAlpineAPK  Format = "alpine"
	FormatAndroidAPK Format = "android"
	FormatTarball    Format = "tarball"
	FormatAppImage   Format = "appimage"
)

// Metadata contains normalized package metadata across all package formats.
type Metadata struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Release      string   `json:"release"`
	Architecture string   `json:"architecture"`
	Description  string   `json:"description"`
	License      string   `json:"license"`
	Maintainer   string   `json:"maintainer"`
	Vendor       string   `json:"vendor"`
	Homepage     string   `json:"homepage"`
	Section      string   `json:"section"`
	Dependencies []string `json:"dependencies"`
	Provides     []string `json:"provides"`
	Recommends   []string `json:"recommends"`
}

// FileEntry represents a file or symlink inside the package intermediate representation.
type FileEntry struct {
	SourcePath      string      `json:"source_path"`      // Temporary staging file path if on disk
	DestinationPath string      `json:"destination_path"` // Absolute path in host filesystem (e.g., /usr/bin/foo)
	Mode            fs.FileMode `json:"mode"`
	Owner           string      `json:"owner"`
	Group           string      `json:"group"`
	ModTime         time.Time   `json:"mod_time"`
	IsSymlink       bool        `json:"is_symlink"`
	SymlinkTarget   string      `json:"symlink_target"`
	IsDir           bool        `json:"is_dir"`
	Elf             *ElfInfo    `json:"elf,omitempty"`
}

// ElfInfo contains extracted ELF binary properties.
type ElfInfo struct {
	Architecture     string   `json:"architecture"`
	Interpreter      string   `json:"interpreter"`
	DynamicLibraries []string `json:"dynamic_libraries"`
	RPath            []string `json:"rpath"`
	RunPath          []string `json:"runpath"`
	Soname           string   `json:"soname"`
	NeedsMusl        bool     `json:"needs_musl"`
	NeedsGlibc       bool     `json:"needs_glibc"`
}

// Scripts contains maintainer lifecycle scripts.
type Scripts struct {
	PreInstall  string `json:"pre_install,omitempty"`
	PostInstall string `json:"post_install,omitempty"`
	PreRemove   string `json:"pre_remove,omitempty"`
	PostRemove  string `json:"post_remove,omitempty"`
}

// PackageIR represents the complete Intermediate Representation of an unpacked package.
type PackageIR struct {
	Format     Format               `json:"format"`
	Metadata   Metadata             `json:"metadata"`
	Files      map[string]FileEntry `json:"files"` // Keyed by destination path
	Scripts    Scripts              `json:"scripts"`
	StagingDir string               `json:"staging_dir"` // Temp dir on disk containing extracted files
}

// NewPackageIR initializes an empty PackageIR with staging directory.
func NewPackageIR(stagingDir string, format Format) *PackageIR {
	return &PackageIR{
		Format:     format,
		Files:      make(map[string]FileEntry),
		StagingDir: stagingDir,
	}
}
