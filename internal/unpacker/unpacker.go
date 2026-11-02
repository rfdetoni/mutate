package unpacker

import (
	"archive/tar"
	"compress/gzip"
	"errors"
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
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		// Security: clean and verify path does not escape targetDir
		cleanName := filepath.Clean(hdr.Name)
		if strings.HasPrefix(cleanName, "/") {
			cleanName = strings.TrimPrefix(cleanName, "/")
		}
		if strings.HasPrefix(cleanName, "../") || cleanName == ".." {
			return fmt.Errorf("path traversal attempt in archive: %s", hdr.Name)
		}

		destOnDisk := filepath.Join(targetDir, cleanName)
		destInHost := "/" + cleanName

		fileInfo := hdr.FileInfo()
		entry := domain.FileEntry{
			SourcePath:      destOnDisk,
			DestinationPath: destInHost,
			Mode:            fileInfo.Mode(),
			ModTime:         hdr.ModTime,
			Owner:           hdr.Uname,
			Group:           hdr.Gname,
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			entry.IsDir = true
			if err := os.MkdirAll(destOnDisk, 0755); err != nil {
				return fmt.Errorf("mkdir %s: %w", destOnDisk, err)
			}

		case tar.TypeSymlink:
			entry.IsSymlink = true
			entry.SymlinkTarget = hdr.Linkname
			if err := os.MkdirAll(filepath.Dir(destOnDisk), 0755); err != nil {
				return err
			}
			os.Remove(destOnDisk)
			if err := os.Symlink(hdr.Linkname, destOnDisk); err != nil {
				return fmt.Errorf("symlink %s -> %s: %w", destOnDisk, hdr.Linkname, err)
			}

		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(destOnDisk), 0755); err != nil {
				return err
			}
			outFile, err := os.OpenFile(destOnDisk, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fileInfo.Mode().Perm())
			if err != nil {
				return fmt.Errorf("create file %s: %w", destOnDisk, err)
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return fmt.Errorf("write file %s: %w", destOnDisk, err)
			}
			outFile.Close()

		default:
			// Ignore unsupported special entries
			continue
		}

		if ir != nil {
			ir.Files[destInHost] = entry
		}
	}

	return nil
}

// SanitizeStagingPath safely computes staging destination without path traversal.
func SanitizeStagingPath(baseDir, relativePath string) (string, error) {
	clean := filepath.Clean(relativePath)
	if strings.HasPrefix(clean, "/") {
		clean = strings.TrimPrefix(clean, "/")
	}
	if strings.HasPrefix(clean, "../") || clean == ".." {
		return "", errors.New("path escapes staging root")
	}
	return filepath.Join(baseDir, clean), nil
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

