package unpacker

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cavaliergopher/cpio"
	"github.com/cavaliergopher/rpm"
	"github.com/rfdetoni/mutate/internal/domain"
)

type RpmUnpacker struct{}

func NewRpmUnpacker() *RpmUnpacker {
	return &RpmUnpacker{}
}

func (u *RpmUnpacker) Unpack(srcPath string, stagingDir string) (*domain.PackageIR, error) {
	f, err := os.Open(srcPath)
	if err != nil {
		return nil, fmt.Errorf("open rpm %s: %w", srcPath, err)
	}
	defer f.Close()

	// Read RPM header and metadata. The file cursor will be positioned right at the payload.
	pkg, err := rpm.Read(f)
	if err != nil {
		return nil, fmt.Errorf("read rpm header: %w", err)
	}

	ir := domain.NewPackageIR(stagingDir, domain.FormatRPM)
	ir.Metadata = domain.Metadata{
		Name:         pkg.Name(),
		Version:      pkg.Version(),
		Release:      pkg.Release(),
		Architecture: pkg.Architecture(),
		Description:  pkg.Description(),
		License:      pkg.License(),
		Vendor:       pkg.Vendor(),
		Homepage:     pkg.URL(),
	}

	for _, req := range pkg.Requires() {
		ir.Metadata.Dependencies = append(ir.Metadata.Dependencies, req.Name())
	}
	for _, prov := range pkg.Provides() {
		ir.Metadata.Provides = append(ir.Metadata.Provides, prov.Name())
	}

	// Extract scripts from RPM header tags (1023=prein, 1024=postin, 1025=preun, 1026=postun)
	if tag := pkg.Header.GetTag(1023); tag != nil {
		ir.Scripts.PreInstall = tag.String()
	}
	if tag := pkg.Header.GetTag(1024); tag != nil {
		ir.Scripts.PostInstall = tag.String()
	}
	if tag := pkg.Header.GetTag(1025); tag != nil {
		ir.Scripts.PreRemove = tag.String()
	}
	if tag := pkg.Header.GetTag(1026); tag != nil {
		ir.Scripts.PostRemove = tag.String()
	}

	// Decompress and unpack payload
	compression := pkg.PayloadCompression()
	if compression == "" {
		compression = "gzip"
	}

	decomp, err := DecompressStream(f, compression)
	if err != nil {
		return nil, fmt.Errorf("decompress rpm payload (%s): %w", compression, err)
	}
	defer decomp.Close()

	cpioReader := cpio.NewReader(decomp)
	for {
		hdr, err := cpioReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read cpio header: %w", err)
		}

		cleanName := filepath.Clean(hdr.Name)
		if strings.HasPrefix(cleanName, ".") {
			cleanName = strings.TrimPrefix(cleanName, ".")
		}
		if strings.HasPrefix(cleanName, "/") {
			cleanName = strings.TrimPrefix(cleanName, "/")
		}
		if strings.HasPrefix(cleanName, "../") || cleanName == ".." {
			return nil, fmt.Errorf("path traversal attempt in rpm cpio: %s", hdr.Name)
		}

		destOnDisk := filepath.Join(stagingDir, cleanName)
		destInHost := "/" + cleanName

		entry := domain.FileEntry{
			SourcePath:      destOnDisk,
			DestinationPath: destInHost,
			Mode:            os.FileMode(hdr.Mode & 0777),
			ModTime:         hdr.ModTime,
		}

		fileType := hdr.Mode & 0170000
		isDir := fileType == 0040000
		isSymlink := fileType == 0120000 && hdr.Linkname != ""

		if isDir {
			entry.IsDir = true
			if err := os.MkdirAll(destOnDisk, 0755); err != nil {
				return nil, fmt.Errorf("mkdir %s: %w", destOnDisk, err)
			}
		} else if isSymlink {
			entry.IsSymlink = true
			entry.SymlinkTarget = hdr.Linkname
			if err := os.MkdirAll(filepath.Dir(destOnDisk), 0755); err != nil {
				return nil, err
			}
			os.Remove(destOnDisk)
			if err := os.Symlink(hdr.Linkname, destOnDisk); err != nil {
				return nil, fmt.Errorf("symlink %s -> %s: %w", destOnDisk, hdr.Linkname, err)
			}
		} else {
			if err := os.MkdirAll(filepath.Dir(destOnDisk), 0755); err != nil {
				return nil, err
			}
			outFile, err := os.OpenFile(destOnDisk, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode&0777))
			if err != nil {
				return nil, fmt.Errorf("create file %s: %w", destOnDisk, err)
			}
			if _, err := io.Copy(outFile, cpioReader); err != nil {
				outFile.Close()
				return nil, fmt.Errorf("write cpio file %s: %w", destOnDisk, err)
			}
			outFile.Close()
		}

		ir.Files[destInHost] = entry
	}

	return ir, nil
}
