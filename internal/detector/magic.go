package detector

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/rfdetoni/mutate/internal/domain"
)

var (
	magicRPM      = []byte{0xed, 0xab, 0xee, 0xdb}
	magicDEB      = []byte("!<arch>\n")
	magicZstd     = []byte{0x28, 0xb5, 0x2f, 0xfd}
	magicGzip     = []byte{0x1f, 0x8b}
	magicZip      = []byte{0x50, 0x4b, 0x03, 0x04}
	magicELF      = []byte{0x7f, 'E', 'L', 'F'}
	magicAppImage = []byte{0x41, 0x49, 0x02} // at offset 8 of ELF
	magicXZ       = []byte{0xfd, 0x37, 0x7a, 0x58, 0x5a, 0x00}
)

// DetectFile inspects a file on disk and detects its package format.
func DetectFile(path string) (domain.Format, error) {
	f, err := os.Open(path)
	if err != nil {
		return domain.FormatUnknown, fmt.Errorf("open file for detection: %w", err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return domain.FormatUnknown, fmt.Errorf("stat file: %w", err)
	}

	return DetectReader(f, fi.Size())
}

// DetectReader detects package format by reading magic bytes and metadata headers.
func DetectReader(ra io.ReaderAt, size int64) (domain.Format, error) {
	if size < 4 {
		return domain.FormatUnknown, errors.New("file too small for format detection")
	}

	header := make([]byte, 16)
	n, err := ra.ReadAt(header, 0)
	if err != nil && err != io.EOF && n < 4 {
		return domain.FormatUnknown, fmt.Errorf("read header: %w", err)
	}

	// 1. RPM: ED AB EE DB
	if len(header) >= 4 && bytes.Equal(header[:4], magicRPM) {
		return domain.FormatRPM, nil
	}

	// 2. DEB: !<arch>\n (21 3C 61 72 63 68 3E)
	if len(header) >= 7 && bytes.Equal(header[:7], magicDEB[:7]) {
		return domain.FormatDEB, nil
	}

	// 3. AppImage: ELF (0x7F 'E' 'L' 'F') with 0x41 0x49 0x02 at offset 8
	if len(header) >= 11 && bytes.Equal(header[:4], magicELF) {
		if bytes.Equal(header[8:11], magicAppImage) {
			return domain.FormatAppImage, nil
		}
	}

	// 4. Zstandard (Arch .pkg.tar.zst): 28 B5 2F FD
	if len(header) >= 4 && bytes.Equal(header[:4], magicZstd) {
		return domain.FormatArch, nil
	}

	// 5. Gzip (1F 8B): Alpine APK or generic tar.gz
	if len(header) >= 2 && bytes.Equal(header[:2], magicGzip) {
		isAlpine, err := checkGzipAlpine(ra)
		if err == nil && isAlpine {
			return domain.FormatAlpineAPK, nil
		}
		return domain.FormatTarball, nil
	}

	// 6. ZIP (50 4B 03 04): Android APK vs Generic ZIP
	if len(header) >= 4 && bytes.Equal(header[:4], magicZip) {
		isAndroid, err := checkZipAndroid(ra, size)
		if err == nil && isAndroid {
			return domain.FormatAndroidAPK, nil
		}
		return domain.FormatTarball, nil
	}

	// 7. XZ tarball: FD 37 7A 58 5A 00
	if len(header) >= 6 && bytes.Equal(header[:6], magicXZ) {
		return domain.FormatTarball, nil
	}

	// 8. Plain Tar Check (ustar at offset 257)
	if size >= 512 {
		tarHeader := make([]byte, 512)
		if _, err := ra.ReadAt(tarHeader, 0); err == nil {
			if bytes.Equal(tarHeader[257:262], []byte("ustar")) {
				return domain.FormatTarball, nil
			}
		}
	}

	return domain.FormatUnknown, nil
}

// checkGzipAlpine checks if the first entry in tar.gz is .PKGINFO.
func checkGzipAlpine(ra io.ReaderAt) (bool, error) {
	sr := io.NewSectionReader(ra, 0, 1024*1024)
	gzr, err := gzip.NewReader(sr)
	if err != nil {
		return false, err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for i := 0; i < 5; i++ {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		if hdr.Name == ".PKGINFO" || hdr.Name == "./.PKGINFO" {
			return true, nil
		}
	}
	return false, nil
}

// checkZipAndroid checks if zip contains AndroidManifest.xml.
func checkZipAndroid(ra io.ReaderAt, size int64) (bool, error) {
	zr, err := zip.NewReader(ra, size)
	if err != nil {
		return false, err
	}

	for _, file := range zr.File {
		if file.Name == "AndroidManifest.xml" {
			return true, nil
		}
	}
	return false, nil
}
