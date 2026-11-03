package resolver

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type ArchResolver struct {
	pacmanLocalDir string
	once           sync.Once
	index          map[string]string
	indexErr       error
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

	return r.resolveViaPacman(soName)
}

func (r *ArchResolver) resolveViaPacman(soName string) (string, error) {
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
	r.once.Do(func() {
		r.index = make(map[string]string)
		entries, err := os.ReadDir(r.pacmanLocalDir)
		if err != nil {
			r.indexErr = err
			return
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			parts := strings.Split(entry.Name(), "-")
			name := entry.Name()
			if len(parts) >= 3 {
				name = strings.Join(parts[:len(parts)-2], "-")
			}
			f, err := os.Open(filepath.Join(r.pacmanLocalDir, entry.Name(), "files"))
			if err != nil {
				continue
			}
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				path := scanner.Text()
				if !strings.Contains(path, "/") {
					continue
				}
				base := filepath.Base(path)
				if !strings.Contains(base, ".so") {
					continue
				}
				if _, ok := r.index[base]; !ok {
					r.index[base] = name
				}
			}
			f.Close()
		}
	})
	if r.indexErr != nil {
		return "", r.indexErr
	}
	if name, ok := r.index[soName]; ok {
		return name, nil
	}
	return "", ErrNotFound
}

 // ResolveMany looks up only requested SONAMEs and stops scanning as soon
 // as all providers are found, instead of indexing all installed libraries.
func (r *ArchResolver) ResolveMany(libs []string) map[string]string{
 found:=make(map[string]string,len(libs))
 wanted:=make(map[string]struct{},len(libs))
 for _,name:=range libs{if name!=""{wanted[name]=struct{}{}}}
 if len(wanted)==0{return found}
 entries,err:=os.ReadDir(r.pacmanLocalDir)
 if err==nil{
scan:
  for _,entry:=range entries{
   if !entry.IsDir(){continue}
   file,err:=os.Open(filepath.Join(r.pacmanLocalDir,entry.Name(),"files"))
   if err!=nil{continue}
   parts:=strings.Split(entry.Name(),"-")
   pkg:=entry.Name()
   if len(parts)>=3{pkg=strings.Join(parts[:len(parts)-2],"-")}
   scanner:=bufio.NewScanner(file)
   for scanner.Scan(){
    path:=scanner.Text()
    if !strings.Contains(path,"/"){continue}
    name:=filepath.Base(path)
    if _,ok:=wanted[name];!ok{continue}
    if _,ok:=found[name];ok{continue}
    found[name]=pkg
    if len(found)==len(wanted){_ =file.Close();break scan}
   }
   _=file.Close()
  }
 }
 for name:=range wanted{
  if _,ok:=found[name];ok{continue}
  if pkg,err:=r.resolveViaPacman(name);err==nil&&pkg!=""{found[name]=pkg}
 }
 return found
}
