package resolver
import (
 "bufio"
 "fmt"
 "os"
 "path/filepath"
 "strings"
)

type AlpineResolver struct { dbPath string }
func NewAlpineResolver()*AlpineResolver{return &AlpineResolver{dbPath:"/lib/apk/db/installed"}}
func (r *AlpineResolver) HostDistro() Distro{return DistroAlpine}
// Resolve uses the installed APK database file ownership records.
func (r *AlpineResolver) Resolve(soName string)(string,error){
 f,err:=os.Open(r.dbPath)
 if err!=nil{return "",fmt.Errorf("open apk database: %w",err)}
 defer f.Close()
 sc:=bufio.NewScanner(f)
 var pkg,dir string
 for sc.Scan(){
  line:=sc.Text()
  if line==""{pkg="";dir="";continue}
  if strings.HasPrefix(line,"P:"){pkg=strings.TrimPrefix(line,"P:")}
  if strings.HasPrefix(line,"F:"){dir=strings.TrimPrefix(line,"F:")}
  if strings.HasPrefix(line,"R:") && filepath.Base(strings.TrimPrefix(line,"R:"))==soName && pkg!="" {
   if strings.HasPrefix(dir,"lib") || strings.HasPrefix(dir,"usr/lib") {return pkg,nil}
  }
 }
 if err:=sc.Err();err!=nil{return "",err}
 return "",fmt.Errorf("%w: %s on alpine",ErrNotFound,soName)
}
