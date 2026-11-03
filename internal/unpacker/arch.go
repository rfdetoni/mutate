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
	"github.com/rfdetoni/mutate/internal/scripts"
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
	budget, err := newExtractionBudget()
	if err != nil {
		return nil, err
	}

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
			data, err := readMetadata(tr)
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
			if err := parseArchInstallScript(string(data), &ir.Scripts); err != nil {
				return nil, fmt.Errorf("parse .INSTALL: %w", err)
			}
			continue

		case ".MTREE", ".BUILDINFO":
			// Ignore packaging metadata
			continue
		}
		if hdr.Typeflag == tar.TypeDir && (cleanName == "." || cleanName == "/") {
			continue
		}
		if hdr.Size > maxEntryBytes {
			return nil, fmt.Errorf("archive entry too large: %q", hdr.Name)
		}
		if err := extractArchiveEntry(stagingDir, ir, hdr.Name, hdr.Typeflag, hdr.Linkname, hdr.FileInfo().Mode(), hdr.ModTime, hdr.Uname, hdr.Gname, tr, budget); err != nil {
			return nil, fmt.Errorf("extract %q: %w", hdr.Name, err)
		}
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

func parseArchInstallScript(raw string, result *domain.Scripts) error {
	hooks, err := scripts.ExtractArchInstall(raw)
	if err != nil {
		return err
	}
	*result = hooks
	return nil
}
