package resolver

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type AlpineResolver struct{ dbPath string }

func NewAlpineResolver() *AlpineResolver     { return &AlpineResolver{dbPath: "/lib/apk/db/installed"} }
func (r *AlpineResolver) HostDistro() Distro { return DistroAlpine }

// Resolve uses the installed APK database file ownership records.
func (r *AlpineResolver) Resolve(soName string) (string, error) {
	f, err := os.Open(r.dbPath)
	if err != nil {
		return "", fmt.Errorf("open apk database: %w", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var pkg, dir string
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			pkg = ""
			dir = ""
			continue
		}
		if strings.HasPrefix(line, "P:") {
			pkg = strings.TrimPrefix(line, "P:")
		}
		if strings.HasPrefix(line, "F:") {
			dir = strings.TrimPrefix(line, "F:")
		}
		if strings.HasPrefix(line, "R:") && filepath.Base(strings.TrimPrefix(line, "R:")) == soName && pkg != "" {
			if strings.HasPrefix(dir, "lib") || strings.HasPrefix(dir, "usr/lib") {
				return pkg, nil
			}
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%w: %s on alpine", ErrNotFound, soName)
}

// ResolveMany scans the Alpine installed database once for all requested
// SONAMEs, rather than once for each external library.
func (r *AlpineResolver) ResolveMany(libs []string) map[string]string {
	found := make(map[string]string, len(libs))
	wanted := make(map[string]struct{}, len(libs))
	for _, name := range libs {
		if name != "" {
			wanted[name] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return found
	}
	file, err := os.Open(r.dbPath)
	if err != nil {
		return found
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	var pkg, dir string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			pkg = ""
			dir = ""
		case strings.HasPrefix(line, "P:"):
			pkg = strings.TrimPrefix(line, "P:")
		case strings.HasPrefix(line, "F:"):
			dir = strings.TrimPrefix(line, "F:")
		case strings.HasPrefix(line, "R:") && pkg != "" && (strings.HasPrefix(dir, "lib") || strings.HasPrefix(dir, "usr/lib")):
			name := filepath.Base(strings.TrimPrefix(line, "R:"))
			if _, ok := wanted[name]; !ok {
				continue
			}
			if _, ok := found[name]; ok {
				continue
			}
			found[name] = pkg
			if len(found) == len(wanted) {
				return found
			}
		}
	}
	return found
}
