package unpacker

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"

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
		fileType := hdr.Mode & 0170000
		var kind byte
		switch fileType {
		case 0040000:
			kind = tar.TypeDir
		case 0120000:
			kind = tar.TypeSymlink
		case 0100000, 0:
			kind = tar.TypeReg
		default:
			return nil, fmt.Errorf("unsupported rpm cpio entry %q (mode %o)", hdr.Name, hdr.Mode)
		}
		if err := extractArchiveEntry(stagingDir, ir, hdr.Name, kind, hdr.Linkname, os.FileMode(hdr.Mode&07777), hdr.ModTime, "", "", cpioReader); err != nil {
			return nil, fmt.Errorf("extract rpm entry %q: %w", hdr.Name, err)
		}
	}

	return ir, nil
}
