package unpacker

import (
 "bytes"
 "debug/elf"
 "encoding/binary"
 "errors"
 "fmt"
 "io"
 "os"
 "os/exec"
 "path/filepath"
 "regexp"
 "strings"

 "github.com/rfdetoni/mutate/internal/domain"
)

type AppImageUnpacker struct{}
func NewAppImageUnpacker()*AppImageUnpacker{return &AppImageUnpacker{}}

// squashfsOffset locates a v4 SquashFS superblock without ever executing the
// AppImage runtime. The ELF header alone is not enough to identify the offset.
func squashfsOffset(f *os.File)(int64,error) {
 st,err:=f.Stat();if err!=nil{return 0,err}
 const block=1<<20
 buffer:=make([]byte,block+96)
 for pos:=int64(0);pos<st.Size();pos+=block {
  n,err:=f.ReadAt(buffer,pos)
  if err!=nil && !errors.Is(err,io.EOF){return 0,err}
  for i:=0;i+96<=n;i++ {
   if !bytes.Equal(buffer[i:i+4],[]byte("hsqs")){continue}
   h:=buffer[i:i+96]
   off:=pos+int64(i)
   if binary.LittleEndian.Uint16(h[28:30])!=4 {continue}
   blockSize:=binary.LittleEndian.Uint32(h[12:16])
   bytesUsed:=binary.LittleEndian.Uint64(h[40:48])
   if blockSize<4096 || blockSize>1<<20 || blockSize&(blockSize-1)!=0 || bytesUsed<96 || uint64(st.Size()-off)<bytesUsed {continue}
   return off,nil
  }
  if n<block {break}
 }
 return 0,fmt.Errorf("no valid SquashFS v4 filesystem found in AppImage")
}

func (u *AppImageUnpacker) Unpack(src,stage string)(*domain.PackageIR,error) {
 f,err:=os.Open(src);if err!=nil{return nil,err}
 offset,err:=squashfsOffset(f);f.Close()
 if err!=nil{return nil,err}
 exe,err:=exec.LookPath("unsquashfs")
 if err!=nil{return nil,fmt.Errorf("AppImage type 2 requires squashfs-tools (unsquashfs): %w",err)}
 base:=strings.TrimSuffix(filepath.Base(src),filepath.Ext(src))
 base=strings.ToLower(regexp.MustCompile(`[^a-zA-Z0-9+.-]`).ReplaceAllString(base,"-"))
 if base=="" || base=="." || base==".." {return nil,fmt.Errorf("invalid AppImage name")}
 if len(base)>64{base=base[:64]}
 fsDir:=filepath.Join(stage,"appdir")
 if err:=os.MkdirAll(fsDir,0700);err!=nil{return nil,err}
 // Extract without running the embedded executable. No shell interpolation.
 cmd:=exec.Command(exe,"-no-progress","-no-xattrs","-offset",fmt.Sprint(offset),"-dest",fsDir,src)
 if out,err:=cmd.CombinedOutput();err!=nil{return nil,fmt.Errorf("unsquashfs failed: %w: %s",err,string(out))}
 appRun:=filepath.Join(fsDir,"AppRun")
 st,err:=os.Lstat(appRun)
 if err!=nil{return nil,fmt.Errorf("AppImage missing AppRun: %w",err)}
 if st.IsDir(){return nil,fmt.Errorf("AppRun must not be a directory")}
 pkgDir:="/opt/"+base
 ir:=domain.NewPackageIR(stage,domain.FormatAppImage)
 ef,err:=elf.Open(src)
 if err!=nil{return nil,fmt.Errorf("read AppImage ELF: %w",err)}
 switch ef.Machine{
 case elf.EM_X86_64:ir.Metadata.Architecture="amd64"
 case elf.EM_AARCH64:ir.Metadata.Architecture="arm64"
 default:ef.Close();return nil,fmt.Errorf("unsupported AppImage ELF architecture: %s",ef.Machine)
 }
 ef.Close()
 ir.Metadata.Name=base
 ir.Metadata.Version="1.0.0"
 ir.Metadata.Release="1"
 ir.Metadata.Description="Repackaged AppImage (runtime compatibility not guaranteed)"
 ir.Metadata.License="Unknown"
 var count int
 err=filepath.Walk(fsDir,func(path string,info os.FileInfo,walkErr error)error{
  if walkErr!=nil{return walkErr}
  if path==fsDir{return nil}
  count++;if count>maxArchiveEntries{return fmt.Errorf("AppImage contains too many files")}
  relative,err:=filepath.Rel(fsDir,path);if err!=nil{return err}
  host:=filepath.ToSlash(filepath.Join(pkgDir,relative))
  entry:=domain.FileEntry{DestinationPath:host,SourcePath:path,Mode:info.Mode(),ModTime:info.ModTime(),IsDir:info.IsDir()}
  if info.Mode()&os.ModeSymlink!=0{
   entry.IsSymlink=true
   target,err:=os.Readlink(path);if err!=nil{return err}
   if filepath.IsAbs(target){return fmt.Errorf("AppImage contains absolute symlink %q -> %q",relative,target)}
   resolved:=filepath.Clean(filepath.Join(filepath.Dir(path),target))
   rel,err:=filepath.Rel(fsDir,resolved)
   if err!=nil || rel==".." || strings.HasPrefix(rel,".."+string(filepath.Separator)){return fmt.Errorf("AppImage symlink escapes root: %q",relative)}
   entry.SymlinkTarget=target
  }
  if info.Mode().IsRegular() && info.Size()>maxEntryBytes{return fmt.Errorf("AppImage entry too large: %q",relative)}
  if !info.IsDir()&&!info.Mode().IsRegular()&&!entry.IsSymlink{return fmt.Errorf("unsupported AppImage file mode: %q",relative)}
  ir.Files[host]=entry
  return nil
 })
 if err!=nil{return nil,err}
 wrapperDest:="/usr/bin/"+base
 wrapperDisk:=filepath.Join(stage,"usr/bin",base)
 if err:=os.MkdirAll(filepath.Dir(wrapperDisk),0755);err!=nil{return nil,err}
 wrapper:=fmt.Sprintf("#!/bin/sh\nset -eu\nAPPDIR='%s'\nexport APPDIR\nexec \"$APPDIR/AppRun\" \"$@\"\n",pkgDir)
 if err:=os.WriteFile(wrapperDisk,[]byte(wrapper),0755);err!=nil{return nil,err}
 ir.Files[wrapperDest]=domain.FileEntry{DestinationPath:wrapperDest,SourcePath:wrapperDisk,Mode:0755}
 return ir,nil
}
