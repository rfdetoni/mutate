package unpacker

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/blakesmith/ar"
	"github.com/klauspost/compress/zstd"
)

func TestDebUnpacker(t *testing.T) {
	// Create a synthetic .deb in memory
	var arBuf bytes.Buffer
	aw := ar.NewWriter(&arBuf)
	aw.WriteGlobalHeader()

	// 1. debian-binary
	hdrBin := &ar.Header{
		Name:    "debian-binary",
		Size:    4,
		Mode:    0644,
		ModTime: time.Now(),
	}
	aw.WriteHeader(hdrBin)
	aw.Write([]byte("2.0\n"))

	// 2. control.tar.gz
	var ctlTarBuf bytes.Buffer
	gzwCtl := gzip.NewWriter(&ctlTarBuf)
	twCtl := tar.NewWriter(gzwCtl)

	ctlContent := "Package: mytest\nVersion: 1.2.3-4\nArchitecture: amd64\nDescription: test pkg\nDepends: libc6, libssl3\n"
	twCtl.WriteHeader(&tar.Header{
		Name: "./control",
		Mode: 0644,
		Size: int64(len(ctlContent)),
	})
	twCtl.Write([]byte(ctlContent))

	postinstContent := "#!/bin/sh\necho 'installed'\n"
	twCtl.WriteHeader(&tar.Header{
		Name: "./postinst",
		Mode: 0755,
		Size: int64(len(postinstContent)),
	})
	twCtl.Write([]byte(postinstContent))

	twCtl.Close()
	gzwCtl.Close()

	hdrCtl := &ar.Header{
		Name:    "control.tar.gz",
		Size:    int64(ctlTarBuf.Len()),
		Mode:    0644,
		ModTime: time.Now(),
	}
	aw.WriteHeader(hdrCtl)
	aw.Write(ctlTarBuf.Bytes())

	// 3. data.tar.gz
	var dataTarBuf bytes.Buffer
	gzwData := gzip.NewWriter(&dataTarBuf)
	twData := tar.NewWriter(gzwData)

	fileData := "binary-content-here"
	twData.WriteHeader(&tar.Header{
		Name: "usr/bin/mytest",
		Mode: 0755,
		Size: int64(len(fileData)),
	})
	twData.Write([]byte(fileData))

	twData.Close()
	gzwData.Close()

	hdrData := &ar.Header{
		Name:    "data.tar.gz",
		Size:    int64(dataTarBuf.Len()),
		Mode:    0644,
		ModTime: time.Now(),
	}
	aw.WriteHeader(hdrData)
	aw.Write(dataTarBuf.Bytes())

	// Save synthetic .deb to temp file
	tmpDeb, err := os.CreateTemp("", "test-*.deb")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpDeb.Name())
	tmpDeb.Write(arBuf.Bytes())
	tmpDeb.Close()

	stagingDir, err := os.MkdirTemp("", "staging-deb-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stagingDir)

	unpacker := NewDebUnpacker()
	ir, err := unpacker.Unpack(tmpDeb.Name(), stagingDir)
	if err != nil {
		t.Fatalf("Unpack deb failed: %v", err)
	}

	if ir.Metadata.Name != "mytest" {
		t.Errorf("expected name mytest, got %s", ir.Metadata.Name)
	}
	if ir.Metadata.Version != "1.2.3" || ir.Metadata.Release != "4" {
		t.Errorf("expected version 1.2.3-4, got %s-%s", ir.Metadata.Version, ir.Metadata.Release)
	}
	if ir.Scripts.PostInstall != postinstContent {
		t.Errorf("expected postinst content match")
	}
	if _, ok := ir.Files["/usr/bin/mytest"]; !ok {
		t.Errorf("expected /usr/bin/mytest in ir.Files")
	}

	diskPath := filepath.Join(stagingDir, "usr/bin/mytest")
	content, err := os.ReadFile(diskPath)
	if err != nil || string(content) != fileData {
		t.Errorf("expected file on disk with content '%s', got '%s', err: %v", fileData, string(content), err)
	}
}

func TestArchUnpacker(t *testing.T) {
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)

	pkgInfo := "pkgname = archtest\npkgver = 2.0.0-1\narch = x86_64\npkgdesc = arch test package\ndepend = glibc\n"
	tw.WriteHeader(&tar.Header{
		Name: ".PKGINFO",
		Mode: 0644,
		Size: int64(len(pkgInfo)),
	})
	tw.Write([]byte(pkgInfo))

	fileData := "arch-binary"
	tw.WriteHeader(&tar.Header{
		Name: "usr/bin/archtool",
		Mode: 0755,
		Size: int64(len(fileData)),
	})
	tw.Write([]byte(fileData))
	tw.Close()

	var zstBuf bytes.Buffer
	zw, err := zstd.NewWriter(&zstBuf)
	if err != nil {
		t.Fatal(err)
	}
	zw.Write(tarBuf.Bytes())
	zw.Close()

	tmpArch, err := os.CreateTemp("", "test-*.pkg.tar.zst")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpArch.Name())
	tmpArch.Write(zstBuf.Bytes())
	tmpArch.Close()

	stagingDir, err := os.MkdirTemp("", "staging-arch-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stagingDir)

	unpacker := NewArchUnpacker()
	ir, err := unpacker.Unpack(tmpArch.Name(), stagingDir)
	if err != nil {
		t.Fatalf("Unpack arch failed: %v", err)
	}

	if ir.Metadata.Name != "archtest" || ir.Metadata.Version != "2.0.0" {
		t.Errorf("unexpected metadata: %+v", ir.Metadata)
	}
	if _, ok := ir.Files["/usr/bin/archtool"]; !ok {
		t.Errorf("expected /usr/bin/archtool in Files")
	}
}

func TestAlpineUnpacker(t *testing.T) {
	var apkBuf bytes.Buffer

	// Stream 1: Control (.PKGINFO)
	gzw1 := gzip.NewWriter(&apkBuf)
	tw1 := tar.NewWriter(gzw1)
	pkgInfo := "pkgname = alpinetest\npkgver = 3.1.0-r2\narch = x86_64\npkgdesc = alpine pkg\n"
	tw1.WriteHeader(&tar.Header{
		Name: ".PKGINFO",
		Mode: 0644,
		Size: int64(len(pkgInfo)),
	})
	tw1.Write([]byte(pkgInfo))
	tw1.Close()
	gzw1.Close()

	// Stream 2: Data
	gzw2 := gzip.NewWriter(&apkBuf)
	tw2 := tar.NewWriter(gzw2)
	fileData := "alpine-bin"
	tw2.WriteHeader(&tar.Header{
		Name: "usr/bin/alpinetool",
		Mode: 0755,
		Size: int64(len(fileData)),
	})
	tw2.Write([]byte(fileData))
	tw2.Close()
	gzw2.Close()

	tmpApk, err := os.CreateTemp("", "test-*.apk")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpApk.Name())
	tmpApk.Write(apkBuf.Bytes())
	tmpApk.Close()

	stagingDir, err := os.MkdirTemp("", "staging-alpine-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stagingDir)

	unpacker := NewAlpineUnpacker()
	ir, err := unpacker.Unpack(tmpApk.Name(), stagingDir)
	if err != nil {
		t.Fatalf("Unpack alpine failed: %v", err)
	}

	if ir.Metadata.Name != "alpinetest" || ir.Metadata.Version != "3.1.0" || ir.Metadata.Release != "2" {
		t.Errorf("unexpected alpine metadata: %+v", ir.Metadata)
	}
	if _, ok := ir.Files["/usr/bin/alpinetool"]; !ok {
		t.Errorf("expected /usr/bin/alpinetool in Files")
	}
}
