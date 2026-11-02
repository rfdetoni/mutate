package unpacker
import (
 "encoding/binary"
 "io"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestAppImageExtractWithoutExecutingRuntime(t *testing.T){
 source:=filepath.Join(t.TempDir(),"example.AppImage")
 self,err:=os.Executable();if err!=nil{t.Fatal(err)}
 in,err:=os.Open(self);if err!=nil{t.Fatal(err)}
 out,err:=os.Create(source);if err!=nil{t.Fatal(err)}
 if _,err:=io.Copy(out,in);err!=nil{t.Fatal(err)}
 in.Close()
 if _,err:=out.WriteAt([]byte{'A','I',2},8);err!=nil{t.Fatal(err)}
 st,err:=out.Stat();if err!=nil{t.Fatal(err)}
 superblock:=make([]byte,256)
 copy(superblock,[]byte("hsqs"))
 binary.LittleEndian.PutUint16(superblock[28:],4)
 binary.LittleEndian.PutUint32(superblock[12:],131072)
 binary.LittleEndian.PutUint64(superblock[40:],128)
 if _,err:=out.WriteAt(superblock,st.Size());err!=nil{t.Fatal(err)}
 if err:=out.Close();err!=nil{t.Fatal(err)}
 bin:=t.TempDir()
 stub:=filepath.Join(bin,"unsquashfs")
 script:=`#!/bin/sh
while [ "$#" -gt 0 ]; do
 if [ "$1" = "-dest" ]; then
  shift
  mkdir -p "$1/usr/bin"
  printf '#!/bin/sh\necho hi\n' > "$1/AppRun"
  chmod +x "$1/AppRun"
  printf payload > "$1/usr/bin/example"
  exit 0
 fi
 shift
done
exit 2
`
 if err:=os.WriteFile(stub,[]byte(script),0755);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",bin+":"+os.Getenv("PATH"))
 stage:=t.TempDir()
 ir,err:=NewAppImageUnpacker().Unpack(source,stage)
 if err!=nil{t.Fatal(err)}
 if _,ok:=ir.Files["/opt/example/usr/bin/example"];!ok{t.Fatal("missing extracted file")}
 wrapper,err:=os.ReadFile(filepath.Join(stage,"usr/bin/example"))
 if err!=nil{t.Fatal(err)}
 if !strings.Contains(string(wrapper),"APPDIR='/opt/example'") || !strings.Contains(string(wrapper),"exec \"$APPDIR/AppRun\""){t.Fatalf("invalid wrapper: %s",wrapper)}
}
