package resolver

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type ArchResolver struct {
	pacmanLocalDir string
}

func NewArchResolver() *ArchResolver {
	return &ArchResolver{
		pacmanLocalDir: "/var/lib/pacman/local",
	}
}

func (r *ArchResolver) HostDistro() Distro {
	return DistroArch
}

func (r *ArchResolver) Resolve(soName string) (string, error) {
	// 1. Scan /var/lib/pacman/local/*/files directly
	pkg, err := r.searchPacmanLocal(soName)
	if err == nil && pkg != "" {
		return pkg, nil
	}

	// 2. Fallback to `pacman -F <soName>`
	if path, err := exec.LookPath("pacman"); err == nil && path != "" {
		cmd := exec.Command("pacman", "-F", "--machinereadable", soName)
		out, err := cmd.Output()
		if err == nil {
			scanner := bufio.NewScanner(strings.NewReader(string(out)))
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					// format: repo/pkgname version path
					pkgParts := strings.Split(parts[0], "/")
					if len(pkgParts) == 2 {
						return pkgParts[1], nil
					}
					return parts[0], nil
				}
			}
		}
	}

	return "", fmt.Errorf("%w: %s on arch", ErrNotFound, soName)
}

func (r *ArchResolver) searchPacmanLocal(soName string) (string, error) {
	entries, err := os.ReadDir(r.pacmanLocalDir)
	if err != nil {
		return "", err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		filesPath := filepath.Join(r.pacmanLocalDir, entry.Name(), "files")
		f, err := os.Open(filesPath)
		if err != nil {
			continue
		}

		found := false
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasSuffix(line, "/"+soName) || line == soName {
				found = true
				break
			}
		}
		f.Close()

		if found {
			// Directory name format: <pkgname>-<pkgver>-<pkgrel>
			dirName := entry.Name()
			parts := strings.Split(dirName, "-")
			if len(parts) >= 3 {
				return strings.Join(parts[:len(parts)-2], "-"), nil
			}
			return dirName, nil
		}
	}

	return "", ErrNotFound
}
