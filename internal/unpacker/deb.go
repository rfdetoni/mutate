package unpacker

import (
	"archive/tar"
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/blakesmith/ar"
	"github.com/rfdetoni/mutate/internal/domain"
)

type DebUnpacker struct{}

func NewDebUnpacker() *DebUnpacker {
	return &DebUnpacker{}
}

func (u *DebUnpacker) Unpack(srcPath string, stagingDir string) (*domain.PackageIR, error) {
	f, err := os.Open(srcPath)
	if err != nil {
		return nil, fmt.Errorf("open deb %s: %w", srcPath, err)
	}
	defer f.Close()

	arReader := ar.NewReader(f)
	ir := domain.NewPackageIR(stagingDir, domain.FormatDEB)

	for {
		header, err := arReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read ar entry: %w", err)
		}

		entryName := strings.TrimSpace(header.Name)

		if strings.HasPrefix(entryName, "control.tar") {
			comp := getTarCompression(entryName)
			decomp, err := DecompressStream(arReader, comp)
			if err != nil {
				return nil, fmt.Errorf("decompress %s: %w", entryName, err)
			}
			if err := u.parseControlTar(decomp, ir); err != nil {
				decomp.Close()
				return nil, fmt.Errorf("parse %s: %w", entryName, err)
			}
			decomp.Close()
		} else if strings.HasPrefix(entryName, "data.tar") {
			comp := getTarCompression(entryName)
			decomp, err := DecompressStream(arReader, comp)
			if err != nil {
				return nil, fmt.Errorf("decompress %s: %w", entryName, err)
			}
			tr := tar.NewReader(decomp)
			if err := ExtractTarStream(tr, stagingDir, ir); err != nil {
				decomp.Close()
				return nil, fmt.Errorf("extract %s: %w", entryName, err)
			}
			decomp.Close()
		}
	}

	return ir, nil
}

func (u *DebUnpacker) parseControlTar(r io.Reader, ir *domain.PackageIR) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		baseName := strings.TrimPrefix(filepathBase(hdr.Name), "./")
		switch baseName {
		case "control":
			content, err := readMetadata(tr)
			if err != nil {
				return err
			}
			parseDebControl(content, &ir.Metadata)
		case "preinst":
			data, err := readMetadata(tr)
			if err != nil {
				return fmt.Errorf("read lifecycle metadata: %w", err)
			}
			ir.Scripts.PreInstall = string(data)
		case "postinst":
			data, err := readMetadata(tr)
			if err != nil {
				return fmt.Errorf("read lifecycle metadata: %w", err)
			}
			ir.Scripts.PostInstall = string(data)
		case "prerm":
			data, err := readMetadata(tr)
			if err != nil {
				return fmt.Errorf("read lifecycle metadata: %w", err)
			}
			ir.Scripts.PreRemove = string(data)
		case "postrm":
			data, err := readMetadata(tr)
			if err != nil {
				return fmt.Errorf("read lifecycle metadata: %w", err)
			}
			ir.Scripts.PostRemove = string(data)
		}
	}
	return nil
}

func parseDebControl(data []byte, meta *domain.Metadata) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var currentKey string
	var descLines []string

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			// Continuation line (usually description)
			if currentKey == "Description" {
				descLines = append(descLines, strings.TrimSpace(line))
			}
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		currentKey = strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch currentKey {
		case "Package":
			meta.Name = val
		case "Version":
			vParts := []string{val}
			if i := strings.LastIndex(val, "-"); i > 0 {
				vParts = []string{val[:i], val[i+1:]}
			}
			meta.Version = vParts[0]
			if len(vParts) > 1 {
				meta.Release = vParts[1]
			} else {
				meta.Release = "1"
			}
		case "Architecture":
			meta.Architecture = val
		case "Maintainer":
			meta.Maintainer = val
		case "Homepage":
			meta.Homepage = val
		case "Section":
			meta.Section = val
		case "Description":
			descLines = append(descLines, val)
		case "Depends":
			meta.Dependencies = parseDebList(val)
		case "Provides":
			meta.Provides = parseDebList(val)
		case "Recommends":
			meta.Recommends = parseDebList(val)
		}
	}

	if len(descLines) > 0 {
		meta.Description = strings.Join(descLines, "\n")
	}
}

func parseDebList(s string) []string {
	raw := strings.Split(s, ",")
	var result []string
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

func getTarCompression(filename string) string {
	if strings.HasSuffix(filename, ".gz") {
		return "gz"
	} else if strings.HasSuffix(filename, ".xz") {
		return "xz"
	} else if strings.HasSuffix(filename, ".zst") {
		return "zst"
	}
	return "none"
}

func filepathBase(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx >= 0 {
		return path[idx+1:]
	}
	return path
}
