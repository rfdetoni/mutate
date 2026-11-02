package unpacker

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
 "strings"

	"github.com/klauspost/compress/zstd"
	"github.com/rfdetoni/mutate/internal/domain"
	"github.com/ulikunitz/xz"
)

// Unpacker parses an input package file and extracts it into a PackageIR.
type Unpacker interface {
	Unpack(srcPath string, stagingDir string) (*domain.PackageIR, error)
}

// Decompressor returns an io.ReadCloser for a given compression type.
func DecompressStream(r io.Reader, compType string) (io.ReadCloser, error) {
	switch compType {
	case "gz", "gzip":
		return gzip.NewReader(r)
	case "xz":
		xzr, err := xz.NewReader(r)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(xzr), nil
	case "zst", "zstd":
		zr, err := zstd.NewReader(r)
		if err != nil {
			return nil, err
		}
		return zr.IOReadCloser(), nil
	case "none", "tar", "":
		return io.NopCloser(r), nil
	default:
		return nil, fmt.Errorf("unsupported compression: %s", compType)
	}
}

// ExtractTarStream extracts a tar stream into targetDir safely preventing ZipSlip/path traversal.
func ExtractTarStream(tr *tar.Reader, targetDir string, ir *domain.PackageIR) error {
 for {
  hdr,err:=tr.Next()
  if err==io.EOF{return nil}
  if err!=nil{return fmt.Errorf("read tar entry: %w",err)}
  if hdr.Typeflag==tar.TypeDir && (filepath.Clean(hdr.Name)=="." || filepath.Clean(hdr.Name)=="/"){continue}
  if hdr.Size>maxEntryBytes{return fmt.Errorf("archive entry too large: %q",hdr.Name)}
  if err:=extractArchiveEntry(targetDir,ir,hdr.Name,hdr.Typeflag,hdr.Linkname,hdr.FileInfo().Mode(),hdr.ModTime,hdr.Uname,hdr.Gname,tr);err!=nil{return fmt.Errorf("extract %q: %w",hdr.Name,err)}
 }
}

// SanitizeStagingPath safely computes staging destination without path traversal.
func SanitizeStagingPath(baseDir, relativePath string) (string, error) {
 dest,_,err:=archiveDestination(baseDir,relativePath,nil)
 return dest,err
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func defaultString(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

