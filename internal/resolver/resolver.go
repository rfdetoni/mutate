package resolver

import (
 "fmt"
	"bufio"
	"errors"
	"os"
	"strings"
)

// Distro represents the host Linux distribution family.
type Distro string

const (
	DistroDebian Distro = "debian" // Debian, Ubuntu, Mint, Pop!_OS
	DistroArch   Distro = "arch"   // Arch, Manjaro, EndeavourOS
	DistroFedora Distro = "fedora" // Fedora, RHEL, CentOS, Rocky
	DistroAlpine Distro = "alpine" // Alpine Linux
	DistroOther  Distro = "other"
)

// DependencyResolver maps an ELF dynamic library (.so) to the host system package name.
type DependencyResolver interface {
	Resolve(soName string) (string, error)
	HostDistro() Distro
}

// DetectHostDistro reads /etc/os-release or /usr/lib/os-release and determines the distro family.
func DetectHostDistro() Distro {
	paths := []string{"/etc/os-release", "/usr/lib/os-release"}
	var content string

	for _, p := range paths {
		if data, err := os.ReadFile(p); err == nil {
			content = string(data)
			break
		}
	}

	if content == "" {
		return DistroOther
	}

	id := ""
	idLike := ""

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "ID=") {
			id = strings.Trim(strings.TrimPrefix(line, "ID="), `"`)
		} else if strings.HasPrefix(line, "ID_LIKE=") {
			idLike = strings.Trim(strings.TrimPrefix(line, "ID_LIKE="), `"`)
		}
	}

	combined := strings.ToLower(id + " " + idLike)
	switch {
	case strings.Contains(combined, "debian") || strings.Contains(combined, "ubuntu"):
		return DistroDebian
	case strings.Contains(combined, "arch") || strings.Contains(combined, "manjaro"):
		return DistroArch
	case strings.Contains(combined, "fedora") || strings.Contains(combined, "rhel") || strings.Contains(combined, "centos"):
		return DistroFedora
	case strings.Contains(combined, "alpine"):
		return DistroAlpine
	default:
		return DistroOther
	}
}

// NewAutoResolver returns the appropriate DependencyResolver for the current host system.
func NewAutoResolver() DependencyResolver {
 resolver,err:=NewResolverFor(DetectHostDistro())
 if err!=nil{return &unsupportedResolver{distro:DetectHostDistro()}}
 return resolver
}
type unsupportedResolver struct {distro Distro}
func (r *unsupportedResolver) Resolve(name string)(string,error){return "",fmt.Errorf("no resolver for %s: %w",r.distro,ErrNotFound)}
func (r *unsupportedResolver) HostDistro() Distro{return r.distro}

var ErrNotFound = errors.New("library not found in host packages")

 // NewResolverFor selects a resolver for a target distribution.
 // Never silently fall back to Debian for unknown hosts.
func NewResolverFor(distro Distro) (DependencyResolver,error) {
 switch distro {
 case DistroDebian:return NewDebianResolver(),nil
 case DistroArch:return NewArchResolver(),nil
 case DistroFedora:return NewFedoraResolver(),nil
 case DistroAlpine:return NewAlpineResolver(),nil
 default:return nil,fmt.Errorf("no dependency resolver for %q",distro)
 }
}
