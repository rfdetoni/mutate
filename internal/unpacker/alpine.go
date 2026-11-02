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
			return nil, fmt.Errorf("invalid or truncated Alpine archive member: %w",err)
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
  if hdr.Typeflag==tar.TypeDir && (cleanName=="." || cleanName=="/"){continue}
  if hdr.Size>maxEntryBytes{return nil,fmt.Errorf("archive entry too large: %q",hdr.Name)}
  if err:=extractArchiveEntry(stagingDir,ir,hdr.Name,hdr.Typeflag,hdr.Linkname,hdr.FileInfo().Mode(),hdr.ModTime,hdr.Uname,hdr.Gname,tr);err!=nil{return nil,fmt.Errorf("extract %q: %w",hdr.Name,err)}
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
