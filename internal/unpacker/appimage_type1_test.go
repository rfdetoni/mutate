package unpacker

import (
 "debug/elf"
 "encoding/binary"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "testing"

 "github.com/rfdetoni/mutate/internal/detector"
 "github.com/rfdetoni/mutate/internal/domain"
)

// A minimal ELF runtime in the ISO9660 system area is enough to test dispatch.
// Type 1 is an ISO filesystem from byte zero, not an ISO appended after ELF.
func minimalType1(t *testing.T, withMarker bool) string {
 t.Helper()
 b:=make([]byte,65536)
 copy(b,[]byte{0x7f,'E','L','F'})
 b[4]=2; b[5]=1; b[6]=1
 if withMarker {copy(b[8:11],[]byte{'A','I',1})}
 binary.LittleEndian.PutUint16(b[16:18],2)
 binary.LittleEndian.PutUint16(b[18:20],uint16(elf.EM_X86_64))
 binary.LittleEndian.PutUint32(b[20:24],1)
 binary.LittleEndian.PutUint16(b[52:54],64)
 b[16*2048]=1
 copy(b[16*2048+1:],[]byte("CD001"))
 b[16*2048+6]=1
 file:=filepath.Join(t.TempDir(),"example.AppImage")
 if err:=os.WriteFile(file,b,0755);err!=nil{t.Fatal(err)}
 return file
}

func TestType1ExtractionWithoutRuntimeExecution(t *testing.T) {
 for _,withMarker:=range []bool{true,false}{
  t.Run(fmt.Sprint("marker=",withMarker),func(t *testing.T){
   source:=minimalType1(t,withMarker)
   kind,err:=detector.DetectFile(source)
   if err!=nil || kind!=domain.FormatAppImage{t.Fatalf("detect: %v %s",err,kind)}
   path:=t.TempDir()
   extractor:=filepath.Join(path,"bsdtar")
   script:=`#!/bin/sh
dest=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -P|--absolute-paths|--insecure) exit 99 ;;
    -C) dest="$2"; shift 2 ;;
    *) shift ;;
  esac
done
[ -n "$dest" ] || exit 98
mkdir -p "$dest/usr/bin"
printf '#!/bin/sh\nexit 0\n' > "$dest/AppRun"
chmod 755 "$dest/AppRun"
printf payload > "$dest/usr/bin/example"
`
   if err:=os.WriteFile(extractor,[]byte(script),0755);err!=nil{t.Fatal(err)}
   t.Setenv("PATH",path+":"+os.Getenv("PATH"))
   stage:=t.TempDir()
   ir,err:=NewAppImageUnpacker().Unpack(source,stage)
   if err!=nil{t.Fatal(err)}
   if ir.Metadata.Architecture!="amd64" || ir.Metadata.Name!="example"{t.Fatalf("metadata: %+v",ir.Metadata)}
   if _,ok:=ir.Files["/opt/example/usr/bin/example"];!ok{t.Fatal("ISO payload not indexed")}
   if _,ok:=ir.Files["/usr/bin/example"];!ok{t.Fatal("launcher not created")}
   wrapper,err:=os.ReadFile(filepath.Join(stage,"usr/bin/example"))
   if err!=nil{t.Fatal(err)}
   if !strings.Contains(string(wrapper),"APPDIR='/opt/example'"){t.Fatalf("invalid launcher: %s",wrapper)}
  })
 }
}

func TestType1RequiresBsdtar(t *testing.T) {
 src:=minimalType1(t,true)
 t.Setenv("PATH",t.TempDir())
 _,err:=NewAppImageUnpacker().Unpack(src,t.TempDir())
 if err==nil || !strings.Contains(err.Error(),"bsdtar"){t.Fatalf("expected missing extractor error: %v",err)}
}

func TestType1RejectsUnsafeSymlink(t *testing.T) {
 src:=minimalType1(t,true)
 bin:=t.TempDir()
 extractor:=filepath.Join(bin,"bsdtar")
 script:=`#!/bin/sh
dest=""
while [ "$#" -gt 0 ]; do
 if [ "$1" = "-C" ]; then dest="$2"; shift 2; else shift; fi
done
ln -s /etc "$dest/escape"
printf '#!/bin/sh\nexit 0\n' > "$dest/AppRun"
`
 if err:=os.WriteFile(extractor,[]byte(script),0755);err!=nil{t.Fatal(err)}
 t.Setenv("PATH",bin+":"+os.Getenv("PATH"))
 if _,err:=NewAppImageUnpacker().Unpack(src,t.TempDir());err==nil{t.Fatal("accepted unsafe AppDir symlink")}
}

// With the real external tools available this test builds an actual ISO9660
// Rock Ridge payload, embeds the ELF runtime, and tests extraction end-to-end.
func TestType1RealISO9660Extraction(t *testing.T){
 gen,err:=exec.LookPath("genisoimage")
 if err!=nil{t.Skip("genisoimage not installed")}
 if _,err:=exec.LookPath("bsdtar");err!=nil{t.Skip("bsdtar not installed")}
 dir:=t.TempDir()
 appdir:=filepath.Join(dir,"AppDir")
 if err:=os.MkdirAll(filepath.Join(appdir,"usr/bin"),0755);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(appdir,"AppRun"),[]byte("#!/bin/sh\nexit 0\n"),0755);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(appdir,"usr/bin","hello"),[]byte("payload"),0755);err!=nil{t.Fatal(err)}
 src:=filepath.Join(dir,"real.AppImage")
 cmd:=exec.Command(gen,"-quiet","-R","-o",src,appdir)
 if out,err:=cmd.CombinedOutput();err!=nil{t.Fatalf("create ISO: %v: %s",err,out)}
 f,err:=os.OpenFile(src,os.O_RDWR,0)
 if err!=nil{t.Fatal(err)}
 hdr:=make([]byte,64)
 copy(hdr,[]byte{0x7f,'E','L','F'})
 hdr[4]=2;hdr[5]=1;hdr[6]=1
 copy(hdr[8:],[]byte{'A','I',1})
 binary.LittleEndian.PutUint16(hdr[16:],2)
 binary.LittleEndian.PutUint16(hdr[18:],uint16(elf.EM_X86_64))
 binary.LittleEndian.PutUint32(hdr[20:],1)
 binary.LittleEndian.PutUint16(hdr[52:],64)
 if _,err:=f.WriteAt(hdr,0);err!=nil{f.Close();t.Fatal(err)}
 if err:=f.Close();err!=nil{t.Fatal(err)}
 stage:=t.TempDir()
 ir,err:=NewAppImageUnpacker().Unpack(src,stage)
 if err!=nil{t.Fatalf("extract actual ISO9660: %v",err)}
 got,err:=os.ReadFile(filepath.Join(stage,"appdir","usr/bin","hello"))
 if err!=nil{t.Fatal(err)}
 if string(got)!="payload"{t.Fatalf("payload corrupted: %q",got)}
 if _,ok:=ir.Files["/opt/real/usr/bin/hello"];!ok{t.Fatal("extracted payload not indexed in IR")}
}
