package prerequisites

import (
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/rfdetoni/mutate/internal/domain"
 "github.com/rfdetoni/mutate/internal/resolver"
)

func appImageFixture(t *testing.T, marker byte) string {
 t.Helper()
 bytes:=make([]byte,64)
 copy(bytes,[]byte{0x7f,'E','L','F'})
 if marker!=0 {copy(bytes[8:],[]byte{'A','I',marker})}
 path:=filepath.Join(t.TempDir(),"app.AppImage")
 if err:=os.WriteFile(path,bytes,0600);err!=nil{t.Fatal(err)}
 return path
}
func hasWarningContaining(notices []Notice,needle string)bool{
 for _,n:=range notices{
  if n.Warning&&strings.Contains(n.Message,needle){return true}
 }
 return false
}
func TestAPKWaydroidMissing(t *testing.T){
 n,err:=Check(domain.FormatAndroidAPK,"",false,HostState{},resolver.DistroDebian)
 if err!=nil{t.Fatal(err)}
 if !hasWarningContaining(n,"Waydroid"){t.Fatalf("missing warning: %+v",n)}
 n,err=Check(domain.FormatAndroidAPK,"",false,HostState{WaydroidExecutable:true},resolver.DistroDebian)
 if err!=nil{t.Fatal(err)}
 if len(n)!=1||n[0].Warning{t.Fatalf("should only inform when Waydroid exists: %+v",n)}
}
func TestType2MissingFuseWarnings(t *testing.T){
 n,err:=Check(domain.FormatAppImage,appImageFixture(t,2),false,HostState{},resolver.DistroDebian)
 if err!=nil{t.Fatal(err)}
 if !hasWarningContaining(n,"/dev/fuse")||!hasWarningContaining(n,"libfuse.so.2"){t.Fatalf("missing FUSE notices: %+v",n)}
 if !strings.Contains(n[1].Message,"libfuse2t64"){t.Fatalf("missing Debian install hint: %+v",n)}
}
func TestType2FusePresent(t *testing.T){
 n,err:=Check(domain.FormatAppImage,appImageFixture(t,2),false,HostState{FuseDevice:true,LegacyFuseLibrary:true},resolver.DistroArch)
 if err!=nil{t.Fatal(err)}
 if len(n)!=1||n[0].Warning{t.Fatalf("no warnings expected: %+v",n)}
}
func TestType2ExtractDoesNotDemandFuse(t *testing.T){
 n,err:=Check(domain.FormatAppImage,appImageFixture(t,2),true,HostState{},resolver.DistroArch)
 if err!=nil{t.Fatal(err)}
 if len(n)!=1||n[0].Warning{t.Fatalf("extraction should not demand runtime FUSE: %+v",n)}
}
func TestType1DoesNotDemandFuse2(t *testing.T){
 for _,marker:=range []byte{1,0}{
  n,err:=Check(domain.FormatAppImage,appImageFixture(t,marker),false,HostState{},resolver.DistroDebian)
  if err!=nil{t.Fatal(err)}
  if len(n)!=1||n[0].Warning||!strings.Contains(n[0].Message,"Type 1"){t.Fatalf("wrong Type 1 warning: %+v",n)}
 }
}
func TestNormalPackagesNoNotices(t *testing.T){
 for _,format:=range []domain.Format{domain.FormatDEB,domain.FormatRPM,domain.FormatAlpineAPK,domain.FormatArch,domain.FormatTarball}{
  n,err:=Check(format,"",false,HostState{},resolver.DistroOther)
  if err!=nil||len(n)!=0{t.Fatalf("unexpected notices for %s: %+v %v",format,n,err)}
 }
}
func TestBadAppImageMarkerReportsError(t *testing.T){
 if _,err:=Check(domain.FormatAppImage,filepath.Join(t.TempDir(),"absent"),false,HostState{},resolver.DistroOther);err==nil{t.Fatal("missing file accepted")}
}
