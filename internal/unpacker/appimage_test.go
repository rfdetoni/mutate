package unpacker

import (
 "encoding/binary"
 "os"
 "path/filepath"
 "testing"
)

func TestSquashfsOffsetValid(t *testing.T){
 file:=filepath.Join(t.TempDir(),"test.AppImage")
 bytes:=make([]byte,512)
 copy(bytes[128:],[]byte("hsqs"))
 binary.LittleEndian.PutUint16(bytes[128+28:],4)
 binary.LittleEndian.PutUint32(bytes[128+12:],131072)
 binary.LittleEndian.PutUint64(bytes[128+40:],128)
 if err:=os.WriteFile(file,bytes,0600);err!=nil{t.Fatal(err)}
 f,err:=os.Open(file);if err!=nil{t.Fatal(err)};defer f.Close()
 off,err:=squashfsOffset(f)
 if err!=nil || off!=128{t.Fatalf("offset=%d err=%v",off,err)}
}
func TestSquashfsOffsetRejectsTruncation(t *testing.T){
 file:=filepath.Join(t.TempDir(),"bad.AppImage")
 b:=make([]byte,180)
 copy(b[128:],[]byte("hsqs"))
 binary.LittleEndian.PutUint16(b[128+28:],4)
 binary.LittleEndian.PutUint32(b[128+12:],131072)
 binary.LittleEndian.PutUint64(b[128+40:],512)
 if err:=os.WriteFile(file,b,0600);err!=nil{t.Fatal(err)}
 f,err:=os.Open(file);if err!=nil{t.Fatal(err)};defer f.Close()
 if _,err:=squashfsOffset(f);err==nil{t.Fatal("accepted truncated squashfs")}
}
