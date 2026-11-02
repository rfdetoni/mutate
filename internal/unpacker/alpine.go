package unpacker

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rfdetoni/mutate/internal/domain"
)

type AlpineUnpacker struct{}

func NewAlpineUnpacker() *AlpineUnpacker {
	return &AlpineUnpacker{}
}

func (u *AlpineUnpacker) Unpack(srcPath string, stagingDir string) (*domain.PackageIR, error) {
	f, err := os.Open(srcPath)
	if err != nil {
		return nil, fmt.Errorf("open alpine apk %s: %w", srcPath, err)
	}
	defer f.Close()

	ir := domain.NewPackageIR(stagingDir, domain.FormatAlpineAPK)
	br := bufio.NewReader(f)

	for {
		gzr, err := gzip.NewReader(br)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// If not a gzip stream anymore or trailing bytes, break
			break
		}
		gzr.Multistream(false) // Handle each member individually

		tr := tar.NewReader(gzr)
		for {
			hdr, err := tr.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("read alpine tar entry: %w", err)
			}

			cleanName := filepath.Clean(hdr.Name)
			if strings.HasPrefix(cleanName, "/") {
				cleanName = strings.TrimPrefix(cleanName, "/")
			}

			// Handle Alpine control entries
			switch cleanName {
			case ".PKGINFO":
				data, err := io.ReadAll(tr)
				if err != nil {
					return nil, fmt.Errorf("read .PKGINFO: %w", err)
				}
				parseAlpinePkgInfo(data, &ir.Metadata)
				continue
			case ".pre-install":
				data, _ := io.ReadAll(tr)
				ir.Scripts.PreInstall = string(data)
				continue
			case ".post-install":
				data, _ := io.ReadAll(tr)
				ir.Scripts.PostInstall = string(data)
				continue
			case ".pre-deinstall":
				data, _ := io.ReadAll(tr)
				ir.Scripts.PreRemove = string(data)
				continue
			case ".post-deinstall":
				data, _ := io.ReadAll(tr)
				ir.Scripts.PostRemove = string(data)
				continue
			}

			if strings.HasPrefix(cleanName, ".SIGN.") {
				// Signature file, skip
				continue
			}

			if strings.HasPrefix(cleanName, "../") || cleanName == ".." {
				return nil, fmt.Errorf("path traversal attempt in alpine apk: %s", hdr.Name)
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

		gzr.Close()
	}

	return ir, nil
}

func parseAlpinePkgInfo(data []byte, meta *domain.Metadata) {
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
			vParts := strings.SplitN(val, "-r", 2)
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
