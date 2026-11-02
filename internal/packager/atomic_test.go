package packager
import (
 "os"
 "path/filepath"
 "testing"
 "github.com/rfdetoni/mutate/internal/domain"
)
func TestBuildFailurePreservesOutput(t *testing.T){
 out:=filepath.Join(t.TempDir(),"target.deb")
 if err:=os.WriteFile(out,[]byte("original"),0600);err!=nil{t.Fatal(err)}
 ir:=domain.NewPackageIR(t.TempDir(),domain.FormatDEB)
 if err:=BuildPackageToFile(ir,"unsupported-format",out);err==nil{t.Fatal("expected failure")}
 b,err:=os.ReadFile(out)
 if err!=nil{t.Fatal(err)}
 if string(b)!="original"{t.Fatalf("output overwritten by failed build: %q",b)}
}
