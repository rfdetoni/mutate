package detector

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"testing"

	"github.com/rfdetoni/mutate/internal/domain"
)

func TestDetectFormats(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected domain.Format
	}{
		{
			name:     "RPM magic bytes",
			data:     []byte{0xed, 0xab, 0xee, 0xdb, 0x01, 0x00, 0x00, 0x00},
			expected: domain.FormatRPM,
		},
		{
			name:     "DEB ar archive",
			data:     []byte("!<arch>\ndebian-binary   1234567890"),
			expected: domain.FormatDEB,
		},
		{
			name:     "Zstandard Arch package",
			data:     []byte{0x28, 0xb5, 0x2f, 0xfd, 0x00, 0x00},
			expected: domain.FormatArch,
		},
		{
			name: "AppImage ELF with 41 49 02 signature",
			data: []byte{
				0x7f, 'E', 'L', 'F', // ELF
				0x02, 0x01, 0x01, 0x00,
				0x41, 0x49, 0x02, // AppImage signature at offset 8
				0x00, 0x00, 0x00, 0x00, 0x00,
			},
			expected: domain.FormatAppImage,
		},
		{
			name:     "XZ archive",
			data:     []byte{0xfd, 0x37, 0x7a, 0x58, 0x5a, 0x00, 0x00, 0x00},
			expected: domain.FormatTarball,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bytes.NewReader(tt.data)
			fmt, err := DetectReader(r, int64(len(tt.data)))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fmt != tt.expected {
				t.Fatalf("expected %v, got %v", tt.expected, fmt)
			}
		})
	}
}

func TestDetectAlpineVsGenericTarball(t *testing.T) {
	// 1. Alpine APK: gzip with .PKGINFO inside
	var bufAlpine bytes.Buffer
	gzw := gzip.NewWriter(&bufAlpine)
	tw := tar.NewWriter(gzw)
	hdr := &tar.Header{
		Name: ".PKGINFO",
		Mode: 0644,
		Size: int64(len("pkgname = test\n")),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("pkgname = test\n")); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gzw.Close()

	rAlpine := bytes.NewReader(bufAlpine.Bytes())
	fmtAlpine, err := DetectReader(rAlpine, int64(bufAlpine.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if fmtAlpine != domain.FormatAlpineAPK {
		t.Fatalf("expected %v, got %v", domain.FormatAlpineAPK, fmtAlpine)
	}

	// 2. Generic tar.gz
	var bufTarGz bytes.Buffer
	gzw2 := gzip.NewWriter(&bufTarGz)
	tw2 := tar.NewWriter(gzw2)
	hdr2 := &tar.Header{
		Name: "readme.txt",
		Mode: 0644,
		Size: int64(len("hello")),
	}
	if err := tw2.WriteHeader(hdr2); err != nil {
		t.Fatal(err)
	}
	if _, err := tw2.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	tw2.Close()
	gzw2.Close()

	rTarGz := bytes.NewReader(bufTarGz.Bytes())
	fmtTarGz, err := DetectReader(rTarGz, int64(bufTarGz.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if fmtTarGz != domain.FormatTarball {
		t.Fatalf("expected %v, got %v", domain.FormatTarball, fmtTarGz)
	}
}

func TestDetectAndroidVsGenericZip(t *testing.T) {
	// 1. Android APK: ZIP with AndroidManifest.xml
	var bufAndroid bytes.Buffer
	zw := zip.NewWriter(&bufAndroid)
	f, err := zw.Create("AndroidManifest.xml")
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("<manifest/>"))
	zw.Close()

	rAndroid := bytes.NewReader(bufAndroid.Bytes())
	fmtAndroid, err := DetectReader(rAndroid, int64(bufAndroid.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if fmtAndroid != domain.FormatAndroidAPK {
		t.Fatalf("expected %v, got %v", domain.FormatAndroidAPK, fmtAndroid)
	}

	// 2. Generic Zip
	var bufZip bytes.Buffer
	zw2 := zip.NewWriter(&bufZip)
	f2, err := zw2.Create("data.txt")
	if err != nil {
		t.Fatal(err)
	}
	f2.Write([]byte("generic data"))
	zw2.Close()

	rZip := bytes.NewReader(bufZip.Bytes())
	fmtZip, err := DetectReader(rZip, int64(bufZip.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if fmtZip != domain.FormatTarball {
		t.Fatalf("expected %v, got %v", domain.FormatTarball, fmtZip)
	}
}
