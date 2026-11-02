package resolver

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type FedoraResolver struct{}

func NewFedoraResolver() *FedoraResolver {
	return &FedoraResolver{}
}

func (r *FedoraResolver) HostDistro() Distro {
	return DistroFedora
}

func (r *FedoraResolver) Resolve(soName string) (string, error) {
	// 1. Check common library paths and rpm -qf
	commonPaths := []string{
		"/usr/lib64/" + soName,
		"/usr/lib/" + soName,
		"/lib64/" + soName,
	}

	for _, p := range commonPaths {
		if _, err := os.Stat(p); err == nil {
			if path, err := exec.LookPath("rpm"); err == nil && path != "" {
				cmd := exec.Command("rpm", "-qf", "--queryformat", "%{NAME}", p)
				out, err := cmd.Output()
				if err == nil && len(out) > 0 {
					return strings.TrimSpace(string(out)), nil
				}
			}
		}
	}

	// 2. Fallback to `dnf provides` or `repoquery`
	if path, err := exec.LookPath("dnf"); err == nil && path != "" {
		cmd := exec.Command("dnf", "provides", "*/"+soName)
		out, err := cmd.Output()
		if err == nil {
			scanner := bufio.NewScanner(strings.NewReader(string(out)))
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if strings.Contains(line, " : ") {
					parts := strings.Split(line, " : ")
					pkgNVRA := strings.TrimSpace(parts[0])
					// format: pkg-name-version-release.arch
					dashIdx := strings.LastIndex(pkgNVRA, "-")
					if dashIdx > 0 {
						dashIdx2 := strings.LastIndex(pkgNVRA[:dashIdx], "-")
						if dashIdx2 > 0 {
							return pkgNVRA[:dashIdx2], nil
						}
					}
					return pkgNVRA, nil
				}
			}
		}
	}

	return "", fmt.Errorf("%w: %s on fedora", ErrNotFound, soName)
}
