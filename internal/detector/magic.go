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
	magicRPM           = []byte{0xed, 0xab, 0xee, 0xdb}
	magicDEB           = []byte("!<arch>\n")
	magicZstd          = []byte{0x28, 0xb5, 0x2f, 0xfd}
	magicGzip          = []byte{0x1f, 0x8b}
	magicZip           = []byte{0x50, 0x4b, 0x03, 0x04}
	magicELF           = []byte{0x7f, 'E', 'L', 'F'}
	magicAppImageType1 = []byte{'A', 'I', 0x01}
	magicAppImageType2 = []byte{'A', 'I', 0x02}
	magicXZ            = []byte{0xfd, 0x37, 0x7a, 0x58, 0x5a, 0x00}
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

	// 3. Type 2 uses an appended SquashFS; Type 1 is an ISO9660 image
	// whose ELF runtime lives inside the ISO system area (starting at byte 0).
	// Some legacy Type 1 files predate the AI\x01 marker; validate their PVD.
	if size >= 11 && bytes.Equal(header[:4], magicELF) {
		if bytes.Equal(header[8:11], magicAppImageType2) {
			return domain.FormatAppImage, nil
		}
		const pvdOffset int64 = 16 * 2048
		if size >= pvdOffset+7 {
			var pvd [7]byte
			if n, err := ra.ReadAt(pvd[:], pvdOffset); err == nil && n == len(pvd) &&
				pvd[0] == 1 && bytes.Equal(pvd[1:6], []byte("CD001")) && pvd[6] == 1 {
				return domain.FormatAppImage, nil
			}
		}
		if bytes.Equal(header[8:11], magicAppImageType1) {
			return domain.FormatUnknown, fmt.Errorf("invalid AppImage Type 1: missing ISO9660 primary volume descriptor")
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
		return domain.FormatUnknown, fmt.Errorf("generic ZIP archives are not source tarballs")
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
