package packager

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/rfdetoni/mutate/internal/resolver"
)

// InstallPackage executes the native package manager command to install the package file.
func InstallPackage(pkgPath string, distro resolver.Distro, dryRun bool) error {
	var cmdName string
	var args []string

	switch distro {
	case resolver.DistroDebian:
		cmdName = "apt-get"
		args = []string{"install", "-y", pkgPath}
	case resolver.DistroArch:
		cmdName = "pacman"
		args = []string{"-U", "--noconfirm", pkgPath}
	case resolver.DistroFedora:
		cmdName = "dnf"
		args = []string{"install", "-y", pkgPath}
	case resolver.DistroAlpine:
		cmdName = "apk"
		args = []string{"add", "--allow-untrusted", pkgPath}
	default:
		return fmt.Errorf("unsupported distribution for direct installation: %s", distro)
	}

	if dryRun {
		fmt.Printf("[dry-run] Would execute: sudo %s %v\n", cmdName, args)
		return nil
	}

	// Check if running as root
	var execCmd *exec.Cmd
	if os.Geteuid() == 0 {
		execCmd = exec.Command(cmdName, args...)
	} else {
		execCmd = exec.Command("sudo", append([]string{cmdName}, args...)...)
	}

	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr
	execCmd.Stdin = os.Stdin

	if err := execCmd.Run(); err != nil {
		return fmt.Errorf("run installation (%s): %w", cmdName, err)
	}

	return nil
}
