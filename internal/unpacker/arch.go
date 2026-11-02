package unpacker

import (
	"archive/tar"
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rfdetoni/mutate/internal/domain"
)

type ArchUnpacker struct{}

func NewArchUnpacker() *ArchUnpacker {
	return &ArchUnpacker{}
}

func (u *ArchUnpacker) Unpack(srcPath string, stagingDir string) (*domain.PackageIR, error) {
	f, err := os.Open(srcPath)
	if err != nil {
		return nil, fmt.Errorf("open arch package %s: %w", srcPath, err)
	}
	defer f.Close()

	comp := "zst"
	if strings.HasSuffix(srcPath, ".xz") {
		comp = "xz"
	} else if strings.HasSuffix(srcPath, ".gz") {
		comp = "gz"
	}

	decomp, err := DecompressStream(f, comp)
	if err != nil {
		return nil, fmt.Errorf("decompress arch package: %w", err)
	}
	defer decomp.Close()

	tr := tar.NewReader(decomp)
	ir := domain.NewPackageIR(stagingDir, domain.FormatArch)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read arch tar entry: %w", err)
		}

		cleanName := filepath.Clean(hdr.Name)
		if strings.HasPrefix(cleanName, "/") {
			cleanName = strings.TrimPrefix(cleanName, "/")
		}

		// Handle Arch metadata files
		switch cleanName {
		case ".PKGINFO":
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("read .PKGINFO: %w", err)
			}
			parseArchPkgInfo(data, &ir.Metadata)
			continue

		case ".INSTALL":
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("read .INSTALL: %w", err)
			}
			parseArchInstallScript(string(data), &ir.Scripts)
			continue

		case ".MTREE", ".BUILDINFO":
			// Ignore packaging metadata
			continue
		}

		if strings.HasPrefix(cleanName, "../") || cleanName == ".." {
			return nil, fmt.Errorf("path traversal attempt in arch package: %s", hdr.Name)
		}

		destOnDisk := filepath.Join(stagingDir, cleanName)
		destInHost := "/" + cleanName

		fileInfo := hdr.FileInfo()
		entry := domain.FileEntry{
			SourcePath:      destOnDisk,
			DestinationPath: destInHost,
			Mode:            fileInfo.Mode(),
			ModTime:         hdr.ModTime,
			Owner:           hdr.Uname,
			Group:           hdr.Gname,
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			entry.IsDir = true
			if err := os.MkdirAll(destOnDisk, 0755); err != nil {
				return nil, fmt.Errorf("mkdir %s: %w", destOnDisk, err)
			}
		case tar.TypeSymlink:
			entry.IsSymlink = true
			entry.SymlinkTarget = hdr.Linkname
			if err := os.MkdirAll(filepath.Dir(destOnDisk), 0755); err != nil {
				return nil, err
			}
			os.Remove(destOnDisk)
			if err := os.Symlink(hdr.Linkname, destOnDisk); err != nil {
				return nil, fmt.Errorf("symlink %s -> %s: %w", destOnDisk, hdr.Linkname, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(destOnDisk), 0755); err != nil {
				return nil, err
			}
			outFile, err := os.OpenFile(destOnDisk, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fileInfo.Mode().Perm())
			if err != nil {
				return nil, fmt.Errorf("create file %s: %w", destOnDisk, err)
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return nil, fmt.Errorf("write file %s: %w", destOnDisk, err)
			}
			outFile.Close()
		}

		ir.Files[destInHost] = entry
	}

	return ir, nil
}

func parseArchPkgInfo(data []byte, meta *domain.Metadata) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "pkgname":
			meta.Name = val
		case "pkgver":
			vParts := strings.SplitN(val, "-", 2)
			meta.Version = vParts[0]
			if len(vParts) > 1 {
				meta.Release = vParts[1]
			} else {
				meta.Release = "1"
			}
		case "pkgdesc":
			meta.Description = val
		case "url":
			meta.Homepage = val
		case "license":
			meta.License = val
		case "arch":
			meta.Architecture = val
		case "depend":
			meta.Dependencies = append(meta.Dependencies, val)
		case "provides":
			meta.Provides = append(meta.Provides, val)
		}
	}
}

func parseArchInstallScript(script string, scripts *domain.Scripts) {
	// Simple mapping of arch install functions to lifecycle scripts
	if strings.Contains(script, "post_install") {
		scripts.PostInstall = script
	}
	if strings.Contains(script, "pre_remove") {
		scripts.PreRemove = script
	}
	if strings.Contains(script, "post_remove") {
		scripts.PostRemove = script
	}
}
