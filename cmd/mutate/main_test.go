package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/blakesmith/ar"
)

func createSampleDeb(t *testing.T, dependencies ...string) string {
	var arBuf bytes.Buffer
	aw := ar.NewWriter(&arBuf)
	aw.WriteGlobalHeader()

	// debian-binary
	aw.WriteHeader(&ar.Header{
		Name:    "debian-binary",
		Size:    4,
		Mode:    0644,
		ModTime: time.Now(),
	})
	aw.Write([]byte("2.0\n"))

	// control.tar.gz
	var ctlTar bytes.Buffer
	gzwCtl := gzip.NewWriter(&ctlTar)
	twCtl := tar.NewWriter(gzwCtl)
	ctl := "Package: sample\nVersion: 1.0.0-1\nArchitecture: amd64\nDescription: Sample CLI test\n"
	if len(dependencies) > 0 {
		ctl += "Depends: " + dependencies[0] + "\n"
	}
	twCtl.WriteHeader(&tar.Header{
		Name: "./control",
		Mode: 0644,
		Size: int64(len(ctl)),
	})
	twCtl.Write([]byte(ctl))
	twCtl.Close()
	gzwCtl.Close()

	aw.WriteHeader(&ar.Header{
		Name:    "control.tar.gz",
		Size:    int64(ctlTar.Len()),
		Mode:    0644,
		ModTime: time.Now(),
	})
	aw.Write(ctlTar.Bytes())

	// data.tar.gz
	var dataTar bytes.Buffer
	gzwData := gzip.NewWriter(&dataTar)
	twData := tar.NewWriter(gzwData)
	scriptContent := "#!/bin/sh\necho hello\n"
	twData.WriteHeader(&tar.Header{
		Name: "usr/bin/sample",
		Mode: 0755,
		Size: int64(len(scriptContent)),
	})
	twData.Write([]byte(scriptContent))
	twData.Close()
	gzwData.Close()

	aw.WriteHeader(&ar.Header{
		Name:    "data.tar.gz",
		Size:    int64(dataTar.Len()),
		Mode:    0644,
		ModTime: time.Now(),
	})
	aw.Write(dataTar.Bytes())

	tmp, err := os.CreateTemp("", "sample-*.deb")
	if err != nil {
		t.Fatal(err)
	}
	tmp.Write(arBuf.Bytes())
	tmp.Close()
	return tmp.Name()
}

func TestE2EInspectAndBuild(t *testing.T) {
	debPath := createSampleDeb(t)
	defer os.Remove(debPath)

	outRpm := filepath.Join(t.TempDir(), "sample-test.rpm")

	// 1. Test build command
	buildCmd := newBuildCmd()
	buildCmd.SetArgs([]string{debPath, "--to", "rpm", "-o", outRpm})
	if err := buildCmd.Execute(); err != nil {
		t.Fatalf("build command failed: %v", err)
	}

	fi, err := os.Stat(outRpm)
	if err != nil || fi.Size() == 0 {
		t.Fatalf("expected output rpm file at %s, err: %v", outRpm, err)
	}

	// 2. Test inspect command
	inspectCmd := newInspectCmd()
	inspectCmd.SetArgs([]string{outRpm})
	if err := inspectCmd.Execute(); err != nil {
		t.Fatalf("inspect command failed on generated rpm: %v", err)
	}
}
