package resolver

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type DebianResolver struct {
	dpkgInfoDir string
}

func NewDebianResolver() *DebianResolver {
	return &DebianResolver{
		dpkgInfoDir: "/var/lib/dpkg/info",
	}
}

func (r *DebianResolver) HostDistro() Distro {
	return DistroDebian
}

func (r *DebianResolver) Resolve(soName string) (string, error) {
	// 1. Direct fast filesystem scan in /var/lib/dpkg/info/*.list
	pkg, err := r.searchDpkgInfo(soName)
	if err == nil && pkg != "" {
		return pkg, nil
	}

	// 2. Fallback to `dpkg -S` command
	if path, err := exec.LookPath("dpkg"); err == nil && path != "" {
		cmd := exec.Command("dpkg", "-S", soName)
		out, err := cmd.Output()
		if err == nil {
			line := strings.TrimSpace(string(out))
			if idx := strings.Index(line, ":"); idx > 0 {
				rawPkg := line[:idx]
				// Handle arch suffix e.g. libc6:amd64 -> libc6
				if colon := strings.Index(rawPkg, ":"); colon > 0 {
					return rawPkg[:colon], nil
				}
				return rawPkg, nil
			}
		}
	}

	return "", fmt.Errorf("%w: %s on debian", ErrNotFound, soName)
}

func (r *DebianResolver) searchDpkgInfo(soName string) (string, error) {
	entries, err := os.ReadDir(r.dpkgInfoDir)
	if err != nil {
		return "", err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".list") {
			continue
		}

		fullPath := filepath.Join(r.dpkgInfoDir, entry.Name())
		f, err := os.Open(fullPath)
		if err != nil {
			continue
		}

		found := false
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasSuffix(line, "/"+soName) || line == soName {
				found = true
				break
			}
		}
		f.Close()

		if found {
			base := strings.TrimSuffix(entry.Name(), ".list")
			// Remove architecture suffix if present: e.g. libssl3:amd64 -> libssl3
			if colonIdx := strings.Index(base, ":"); colonIdx > 0 {
				base = base[:colonIdx]
			}
			return base, nil
		}
	}

	return "", ErrNotFound
}
