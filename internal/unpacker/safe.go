package unpacker

import (
 "archive/tar"
 "errors"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "strings"
 "time"

 "github.com/rfdetoni/mutate/internal/domain"
)

const maxEntryBytes int64 = 8 << 30

// archiveDestination rejects traversal and symlink parents, including paths
// already declared as symlinks by the input package.
func archiveDestination(root, name string, ir *domain.PackageIR) (string, string, error) {
 if strings.ContainsRune(name, 0) { return "", "", fmt.Errorf("invalid archive name") }
 clean:=filepath.Clean(strings.TrimPrefix(name,"/"))
 if clean=="." || filepath.IsAbs(clean) || clean==".." || strings.HasPrefix(clean,".."+string(filepath.Separator)) {
  return "","",fmt.Errorf("archive path escapes staging: %q",name)
 }
 root,err:=filepath.Abs(root)
 if err!=nil{return "","",err}
 dest:=filepath.Join(root,clean)
 for p:=filepath.Dir(dest);;p=filepath.Dir(p){
  st,err:=os.Lstat(p)
  if err==nil{
   if st.Mode()&os.ModeSymlink!=0 || !st.IsDir(){return "","",fmt.Errorf("unsafe archive parent %q",p)}
  }else if !errors.Is(err,os.ErrNotExist){return "","",err}
  if p==root {break}
  if p==filepath.Dir(p){return "","",fmt.Errorf("staging root is not a parent of %q",dest)}
 }
 if ir!=nil {
  for parent:=filepath.Dir(clean);parent!=".";parent=filepath.Dir(parent) {
   if e,ok:=ir.Files["/"+parent];ok && e.IsSymlink{return "","",fmt.Errorf("archive entry traverses package symlink %q",parent)}
  }
 }
 return dest,"/"+clean,nil
}

// The package IR retains symlinks without creating them on the staging disk.
// Source builds may materialize only symlinks whose resolved target stays in staging.
func extractArchiveEntry(root string, ir *domain.PackageIR, name string, kind byte, link string, mode os.FileMode, modified time.Time, owner, group string, data io.Reader) error {
 dest,host,err:=archiveDestination(root,name,ir)
 if err!=nil{return err}
 entry:=domain.FileEntry{SourcePath:dest,DestinationPath:host,Mode:mode,ModTime:modified,Owner:owner,Group:group}
 switch kind {
 case tar.TypeDir:
  entry.IsDir=true
  if st,e:=os.Lstat(dest);e==nil && !st.IsDir(){return fmt.Errorf("directory conflicts with existing file: %s",host)}else if e!=nil && !errors.Is(e,os.ErrNotExist){return e}
  if err:=os.MkdirAll(dest,0755);err!=nil{return err}
 case tar.TypeSymlink:
  entry.IsSymlink=true
  entry.SymlinkTarget=link
  if _,e:=os.Lstat(dest);e==nil{return fmt.Errorf("symlink conflicts with existing file: %q",name)}else if !errors.Is(e,os.ErrNotExist){return e}
  if ir==nil {
   if link=="" || filepath.IsAbs(link){return fmt.Errorf("unsafe source symlink %q -> %q",name,link)}
   rootAbs,err:=filepath.Abs(root);if err!=nil{return err}
   resolved:=filepath.Clean(filepath.Join(filepath.Dir(dest),link))
   rel,err:=filepath.Rel(rootAbs,resolved)
   if err!=nil || rel==".." || strings.HasPrefix(rel,".."+string(filepath.Separator)){return fmt.Errorf("source symlink escapes staging: %q",name)}
   if err:=os.MkdirAll(filepath.Dir(dest),0755);err!=nil{return err}
   if err:=os.Symlink(link,dest);err!=nil{return err}
  }
 case tar.TypeReg,tar.TypeRegA:
  if err:=os.MkdirAll(filepath.Dir(dest),0755);err!=nil{return err}
  if st,e:=os.Lstat(dest);e==nil && !st.Mode().IsRegular(){return fmt.Errorf("file conflicts with existing entry: %q",name)}else if e!=nil && !errors.Is(e,os.ErrNotExist){return e}
  out,err:=os.OpenFile(dest,os.O_CREATE|os.O_WRONLY|os.O_TRUNC,0600)
  if err!=nil{return err}
  n,copyErr:=io.Copy(out,io.LimitReader(data,maxEntryBytes+1))
  closeErr:=out.Close()
  if copyErr!=nil{return fmt.Errorf("extract %q: %w",name,copyErr)}
  if closeErr!=nil{return closeErr}
  if n>maxEntryBytes{return fmt.Errorf("archive entry exceeds %d bytes: %q",maxEntryBytes,name)}
  if err:=os.Chmod(dest,mode.Perm());err!=nil{return err}
 default:
  return fmt.Errorf("unsupported archive entry type %d: %q",kind,name)
 }
 if ir!=nil {
  if old,ok:=ir.Files[host];ok && (old.IsSymlink || entry.IsSymlink){return fmt.Errorf("duplicate/conflicting archive path %q",name)}
  ir.Files[host]=entry
 }
 return nil
}
