package unpacker

import (
 "archive/tar"
 "bytes"
 "os"
 "path/filepath"
 "testing"

 "github.com/rfdetoni/mutate/internal/domain"
)

func archiveWithEntries(t *testing.T, entries []tar.Header) *tar.Reader {
 t.Helper()
 var buf bytes.Buffer
 tw:=tar.NewWriter(&buf)
 for _,hdr:=range entries {
  hdr:=hdr
  if err:=tw.WriteHeader(&hdr);err!=nil{t.Fatal(err)}
  if hdr.Typeflag==tar.TypeReg && hdr.Size>0 {
   if _,err:=tw.Write(bytes.Repeat([]byte("X"),int(hdr.Size)));err!=nil{t.Fatal(err)}
  }
 }
 if err:=tw.Close();err!=nil{t.Fatal(err)}
 return tar.NewReader(&buf)
}

func TestExtractRejectsTraversal(t *testing.T){
 for _,name:=range []string{"../outside","../../outside","./../outside"}{
  t.Run(name,func(t *testing.T){
   parent:=t.TempDir()
   stage:=filepath.Join(parent,"stage")
   if err:=os.Mkdir(stage,0755);err!=nil{t.Fatal(err)}
   ir:=domain.NewPackageIR(stage,domain.FormatDEB)
   tr:=archiveWithEntries(t,[]tar.Header{{Name:name,Typeflag:tar.TypeReg,Size:1,Mode:0644}})
   if err:=ExtractTarStream(tr,stage,ir);err==nil{t.Fatal("expected traversal rejection")}
   if _,err:=os.Stat(filepath.Join(parent,"outside"));!os.IsNotExist(err){t.Fatalf("file escaped stage: %v",err)}
  })
 }
}

func TestExtractRejectsSymlinkParent(t *testing.T){
 parent:=t.TempDir()
 stage:=filepath.Join(parent,"stage")
 if err:=os.Mkdir(stage,0755);err!=nil{t.Fatal(err)}
 tr:=archiveWithEntries(t,[]tar.Header{
  {Name:"usr/lib/redirect",Typeflag:tar.TypeSymlink,Linkname:"../../../../escape",Mode:0777},
  {Name:"usr/lib/redirect/outside",Typeflag:tar.TypeReg,Size:1,Mode:0644},
 })
 if err:=ExtractTarStream(tr,stage,domain.NewPackageIR(stage,domain.FormatDEB));err==nil{t.Fatal("expected symlink ancestry rejection")}
 if _,err:=os.Stat(filepath.Join(parent,"escape"));!os.IsNotExist(err){t.Fatalf("escaped staging: %v",err)}
}

func TestSourceSymlinkOutsideRejected(t *testing.T){
 stage:=t.TempDir()
 tr:=archiveWithEntries(t,[]tar.Header{{Name:"file",Typeflag:tar.TypeSymlink,Linkname:"../../outside"}})
 if err:=ExtractTarStream(tr,stage,nil);err==nil{t.Fatal("expected symlink escape rejection")}
}
