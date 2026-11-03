package unpacker

import (
 "bytes"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestPreserveAppImageBothTypes(t *testing.T) {
 for _,typeID:=range []byte{1,2}{
  t.Run(string(rune('0'+typeID)),func(t *testing.T){
   src:=minimalType1(t,true)
   if typeID==2 {
    f,err:=os.OpenFile(src,os.O_WRONLY,0)
    if err!=nil{t.Fatal(err)}
    if _,err:=f.WriteAt([]byte{'A','I',2},8);err!=nil{t.Fatal(err)}
    if err:=f.Close();err!=nil{t.Fatal(err)}
   }
   original,err:=os.ReadFile(src);if err!=nil{t.Fatal(err)}
   stage:=t.TempDir()
   ir,err:=NewAppImageUnpacker().Preserve(src,stage)
   if err!=nil{t.Fatal(err)}
   if len(ir.Files)!=2{t.Fatalf("expected binary + desktop only, got %d",len(ir.Files))}
   dest:="/opt/mutate/appimages/example/example.AppImage"
   entry,ok:=ir.Files[dest]
   if !ok{t.Fatalf("missing original: %v",ir.Files)}
   if entry.SourcePath!=src || entry.Mode.Perm()!=0755 {
    t.Fatalf("original not preserved: %+v",entry)
   }
   got,err:=os.ReadFile(entry.SourcePath);if err!=nil{t.Fatal(err)}
   if !bytes.Equal(got,original){t.Fatal("original AppImage modified")}
   if _,err:=os.Stat(filepath.Join(stage,"appdir"));!os.IsNotExist(err) {
    t.Fatalf("unexpected unpacked AppDir: %v",err)
   }
   desktopPath:="/usr/share/applications/example.desktop"
   desktop,ok:=ir.Files[desktopPath]
   if !ok{t.Fatal("missing desktop shortcut")}
   b,err:=os.ReadFile(desktop.SourcePath);if err!=nil{t.Fatal(err)}
   if !strings.Contains(string(b),"Exec="+dest+" %U")||!strings.Contains(string(b),"Icon=application-x-executable") {
    t.Fatalf("bad desktop entry: %s",b)
   }
  })
 }
}

func TestAppImagePreserverRejectsSymlink(t *testing.T){
 src:=minimalType1(t,true)
 link:=filepath.Join(t.TempDir(),"linked.AppImage")
 if err:=os.Symlink(src,link);err!=nil{t.Fatal(err)}
 if _,err:=NewAppImageUnpacker().Preserve(link,t.TempDir());err==nil{
  t.Fatal("should reject symlink source")
 }
}
func TestAppImagePreserverRejectsNonAppImage(t *testing.T){
 src:=filepath.Join(t.TempDir(),"random.AppImage")
 if err:=os.WriteFile(src,[]byte("not an AppImage"),0755);err!=nil{t.Fatal(err)}
 if _,err:=NewAppImageUnpacker().Preserve(src,t.TempDir());err==nil{
  t.Fatal("should reject malformed AppImage")
 }
}
