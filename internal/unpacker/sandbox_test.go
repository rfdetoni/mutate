package unpacker
import (
 "os"
 "path/filepath"
 "strings"
 "testing"
)
func TestSandboxBuildFailsClosedWhenUnavailable(t *testing.T){
 stage:=t.TempDir()
 work:=filepath.Join(stage,"work")
 if err:=os.MkdirAll(work,0755);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",t.TempDir())
 err:=runIsolatedBuild(work,stage,"make")
 if err==nil||!strings.Contains(err.Error(),"bubblewrap"){t.Fatalf("expected sandbox requirement: %v",err)}
}
