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
 once sync.Once
 index map[string]string
 indexErr error
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

func (r *ArchResolver) searchPacmanLocal(soName string)(string,error){
 r.once.Do(func(){
  r.index=make(map[string]string)
  entries,err:=os.ReadDir(r.pacmanLocalDir)
  if err!=nil{r.indexErr=err;return}
  for _,entry:=range entries {
   if !entry.IsDir(){continue}
   parts:=strings.Split(entry.Name(),"-")
   name:=entry.Name()
   if len(parts)>=3{name=strings.Join(parts[:len(parts)-2],"-")}
   f,err:=os.Open(filepath.Join(r.pacmanLocalDir,entry.Name(),"files"))
   if err!=nil{continue}
   scanner:=bufio.NewScanner(f)
   for scanner.Scan() {
    path:=scanner.Text()
    if !strings.Contains(path,"/"){continue}
    base:=filepath.Base(path)
    if !strings.Contains(base,".so"){continue}
    if _,ok:=r.index[base];!ok{r.index[base]=name}
   }
   f.Close()
  }
 })
 if r.indexErr!=nil{return "",r.indexErr}
 if name,ok:=r.index[soName];ok{return name,nil}
 return "",ErrNotFound
}
