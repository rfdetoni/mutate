package unpacker

import (
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/rfdetoni/mutate/internal/domain"
)

func TestPrepareUsesSameAppImagePolicyForPreserveAndExtract(t *testing.T){
 src:=minimalType1(t,true)
 opts:=PrepareOptions{PackageName:"rebranded",PackageVersion:"3.4.5"}
 preserved,err:=Prepare(src,domain.FormatAppImage,t.TempDir(),opts)
 if err!=nil{t.Fatal(err)}
 if preserved.Metadata.Name!="rebranded"||preserved.Metadata.Version!="3.4.5"{
  t.Fatalf("wrong preserved identity: %+v",preserved.Metadata)
 }
 if _,ok:=preserved.Files["/opt/mutate/appimages/rebranded/rebranded.AppImage"];!ok{
  t.Fatal("preserved path does not match package name")
 }
 bin:=t.TempDir()
 extractor:=filepath.Join(bin,"bsdtar")
 // A fake extractor is sufficient to validate policy and launcher paths; the
 // real ISO9660/bsdtar integration remains tested separately.
 script:="#!/bin/sh\ndest=''\nwhile [ \"$#\" -gt 0 ]; do\n case \"$1\" in -C) dest=\"$2\"; shift 2;; *) shift;; esac\ndone\nmkdir -p \"$dest/usr/bin\"\nprintf '#!/bin/sh\\nexit 0\\n' > \"$dest/AppRun\"\nchmod 755 \"$dest/AppRun\"\nprintf content > \"$dest/usr/bin/hello\"\n"
 if err:=os.WriteFile(extractor,[]byte(script),0755);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",bin+":"+os.Getenv("PATH"))
 opts.Extract=true
 stage:=t.TempDir()
 extracted,err:=Prepare(src,domain.FormatAppImage,stage,opts)
 if err!=nil{t.Fatal(err)}
 if extracted.Metadata.Name!="rebranded"||extracted.Metadata.Version!="3.4.5"{t.Fatalf("wrong extracted identity: %+v",extracted.Metadata)}
 if _,ok:=extracted.Files["/opt/rebranded/usr/bin/hello"];!ok{t.Fatal("extracted paths use the wrong identity")}
 launcher,ok:=extracted.Files["/usr/bin/rebranded"]
 if !ok{t.Fatal("launcher does not use overridden identity")}
 contents,err:=os.ReadFile(launcher.SourcePath)
 if err!=nil{t.Fatal(err)}
 if !strings.Contains(string(contents),"APPDIR='/opt/rebranded'"){t.Fatalf("inconsistent launcher: %s",contents)}
}

func TestPrepareRejectsUnsupportedOptionsBeforeExtraction(t *testing.T){
 for _,opts:=range []PrepareOptions{{Extract:true},{PackageName:"custom"}} {
  _,err:=Prepare("irrelevant.deb",domain.FormatDEB,t.TempDir(),opts)
  if err==nil{t.Fatalf("accepted unsupported options: %+v",opts)}
 }
}

func TestPrepareRejectsUnsafeIdentityBeforeExecutingExtractor(t *testing.T){
 src:=minimalType1(t,true)
 stage:=t.TempDir()
 _,err:=Prepare(src,domain.FormatAppImage,stage,PrepareOptions{Extract:true,PackageName:"../unsafe"})
 if err==nil{t.Fatal("unsafe identity accepted")}
 if _,err:=os.Stat(filepath.Join(stage,"appdir"));!os.IsNotExist(err){
  t.Fatalf("validation occurred after extraction: %v",err)
 }
}
