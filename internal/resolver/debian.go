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

type DebianResolver struct {
 dpkgInfoDir string
 once sync.Once
 index map[string]string
 indexErr error
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

func (r *DebianResolver) searchDpkgInfo(soName string) (string,error) {
 r.once.Do(func(){
  r.index=make(map[string]string)
  entries,err:=os.ReadDir(r.dpkgInfoDir)
  if err!=nil{r.indexErr=err;return}
  for _,entry:=range entries {
   if entry.IsDir() || !strings.HasSuffix(entry.Name(),".list"){continue}
   name:=strings.TrimSuffix(entry.Name(),".list")
   if i:=strings.Index(name,":");i>0{name=name[:i]}
   f,err:=os.Open(filepath.Join(r.dpkgInfoDir,entry.Name()))
   if err!=nil{continue}
   scanner:=bufio.NewScanner(f)
   for scanner.Scan(){
    base:=filepath.Base(scanner.Text())
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
