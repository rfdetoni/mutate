package fhs

import (
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/rfdetoni/mutate/internal/domain"
 "github.com/rfdetoni/mutate/internal/resolver"
)

func TestNormalizeNeverChangesExternalInputs(t *testing.T){
 stage:=t.TempDir()
 inputs:=t.TempDir()
 lib:=filepath.Join(inputs,"libexternal.so.1")
 desktop:=filepath.Join(inputs,"original.desktop")
 if err:=os.WriteFile(lib,[]byte("unchanged library"),0644);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(desktop,[]byte("Path=/usr/lib64/app\n"),0644);err!=nil{t.Fatal(err)}
 ir:=domain.NewPackageIR(stage,domain.FormatRPM)
 ir.Metadata.Architecture="amd64"
 ir.Files["/usr/lib64/libexternal.so.1"]=domain.FileEntry{
  SourcePath:lib,DestinationPath:"/usr/lib64/libexternal.so.1",Mode:0644,
 }
 ir.Files["/usr/share/applications/original.desktop"]=domain.FileEntry{
  SourcePath:desktop,DestinationPath:"/usr/share/applications/original.desktop",Mode:0644,
 }
 if err:=NormalizePackage(ir,resolver.DistroDebian);err!=nil{t.Fatal(err)}
 libBytes,err:=os.ReadFile(lib)
 if err!=nil||string(libBytes)!="unchanged library"{t.Fatalf("external library was moved or changed: %v %q",err,libBytes)}
 desktopBytes,err:=os.ReadFile(desktop)
 if err!=nil||string(desktopBytes)!="Path=/usr/lib64/app\n"{t.Fatalf("external desktop modified: %v %q",err,desktopBytes)}
 libEntry,ok:=ir.Files["/usr/lib/x86_64-linux-gnu/libexternal.so.1"]
 if !ok||!insideStaging(stage,libEntry.SourcePath){t.Fatalf("library not relocated into private staging: %+v",libEntry)}
 copyBytes,err:=os.ReadFile(libEntry.SourcePath)
 if err!=nil||string(copyBytes)!="unchanged library"{t.Fatalf("relocated content: %v %q",err,copyBytes)}
 deskEntry:=ir.Files["/usr/share/applications/original.desktop"]
 if !insideStaging(stage,deskEntry.SourcePath){t.Fatalf("config source was not copy-on-write: %+v",deskEntry)}
 edited,err:=os.ReadFile(deskEntry.SourcePath)
 if err!=nil||!strings.Contains(string(edited),"/usr/lib/x86_64-linux-gnu/app"){
  t.Fatalf("config not rewritten in staging: %v %q",err,edited)
 }
}

func TestNormalizeCollisionFailsWithoutEditingFiles(t *testing.T){
 stage:=t.TempDir()
 external:=filepath.Join(t.TempDir(),"library")
 if err:=os.WriteFile(external,[]byte("source"),0644);err!=nil{t.Fatal(err)}
 ir:=domain.NewPackageIR(stage,domain.FormatRPM)
 ir.Metadata.Architecture="amd64"
 ir.Files["/usr/lib64/libsame.so"]=domain.FileEntry{SourcePath:external,DestinationPath:"/usr/lib64/libsame.so",Mode:0644}
 ir.Files["/usr/lib/x86_64-linux-gnu/libsame.so"]=domain.FileEntry{SourcePath:external,DestinationPath:"/usr/lib/x86_64-linux-gnu/libsame.so",Mode:0644}
 if err:=NormalizePackage(ir,resolver.DistroDebian);err==nil||!strings.Contains(err.Error(),"collision"){
  t.Fatalf("collision not detected: %v",err)
 }
 original,err:=os.ReadFile(external)
 if err!=nil||string(original)!="source"{t.Fatalf("source changed after collision: %v %q",err,original)}
 if _,err:=os.Stat(filepath.Join(stage,"usr/lib/x86_64-linux-gnu/libsame.so"));!os.IsNotExist(err){
  t.Fatalf("wrote staging files before collision validation: %v",err)
 }
 if len(ir.Files)!=2{t.Fatal("package representation mutated on collision")}
}
