package fhs

import (
 "bytes"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "sort"
 "strings"

 "github.com/rfdetoni/mutate/internal/domain"
 "github.com/rfdetoni/mutate/internal/resolver"
)

// TargetFHS defines the library layout of a target distro.
type TargetFHS struct {
 PrimaryLibDir string
}

func GetTargetFHS(distro resolver.Distro,arch string) TargetFHS {
 switch distro {
 case resolver.DistroDebian:
  switch arch {
  case "x86_64","amd64":return TargetFHS{PrimaryLibDir:"/usr/lib/x86_64-linux-gnu"}
  case "aarch64","arm64":return TargetFHS{PrimaryLibDir:"/usr/lib/aarch64-linux-gnu"}
  }
 case resolver.DistroFedora:
  if arch=="x86_64"||arch=="amd64"||arch=="aarch64"||arch=="arm64"{
   return TargetFHS{PrimaryLibDir:"/usr/lib64"}
  }
 }
 return TargetFHS{PrimaryLibDir:"/usr/lib"}
}

func relocatedLibraryPath(dest,libDir string)string{
 for _,prefix:=range []string{
  "/usr/lib64/","/usr/lib/x86_64-linux-gnu/","/usr/lib/aarch64-linux-gnu/",
 }{
  if strings.HasPrefix(dest,prefix) && libDir+"/"!=prefix{
   return libDir+"/"+strings.TrimPrefix(dest,prefix)
  }
 }
 return dest
}

// insideStaging distinguishes owned temporary payloads from immutable user
// inputs (AppImage/APK or explicitly supplied library and configuration files).
func insideStaging(stage,path string)bool{
 if stage==""||path==""{return false}
 root,err:=filepath.Abs(stage)
 if err!=nil{return false}
 file,err:=filepath.Abs(path)
 if err!=nil{return false}
 rel,err:=filepath.Rel(root,file)
 return err==nil&&rel!=".."&&!strings.HasPrefix(rel,".."+string(filepath.Separator))
}

func stageSource(ir *domain.PackageIR,entry domain.FileEntry,dest string)string{
 if entry.SourcePath!=""{return entry.SourcePath}
 if ir.StagingDir==""{return ""}
 return filepath.Join(ir.StagingDir,strings.TrimPrefix(dest,"/"))
}

// copyIntoStage implements copy-on-write for inputs outside staging.
// Only the private copy may be relocated or rewritten by the normalizer.
func copyIntoStage(source,target string)error{
 st,err:=os.Lstat(source)
 if err!=nil{return err}
 if !st.Mode().IsRegular(){return fmt.Errorf("input is not a regular file: %q",source)}
 if err:=os.MkdirAll(filepath.Dir(target),0755);err!=nil{return err}
 in,err:=os.Open(source)
 if err!=nil{return err}
 defer in.Close()
 out,err:=os.OpenFile(target,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600)
 if err!=nil{return err}
 if _,err:=io.Copy(out,in);err!=nil{_ =out.Close();_ =os.Remove(target);return err}
 if err:=out.Close();err!=nil{_ =os.Remove(target);return err}
 if err:=os.Chmod(target,st.Mode().Perm());err!=nil{return err}
 return nil
}

func writeOwnedConfig(source string,content []byte)error{
 st,err:=os.Lstat(source)
 if err!=nil{return err}
 if !st.Mode().IsRegular(){return fmt.Errorf("configuration is not a regular file: %q",source)}
 tmp,err:=os.CreateTemp(filepath.Dir(source),".mutate-config-*")
 if err!=nil{return err}
 defer os.Remove(tmp.Name())
 if _,err:=tmp.Write(content);err!=nil{_ =tmp.Close();return err}
 if err:=tmp.Chmod(st.Mode().Perm());err!=nil{_ =tmp.Close();return err}
 if err:=tmp.Close();err!=nil{return err}
 return os.Rename(tmp.Name(),source)
}

func updateConfigFilePaths(ir *domain.PackageIR,entry domain.FileEntry,dest string,replacements map[string]string)(domain.FileEntry,error){
 source:=stageSource(ir,entry,dest)
 if source==""{return entry,fmt.Errorf("missing source for configuration %q",dest)}
 original,err:=os.ReadFile(source)
 if err!=nil{return entry,err}
 keys:=make([]string,0,len(replacements))
 for key:=range replacements{keys=append(keys,key)}
 // Longer paths take precedence over directory prefixes; order is stable.
 sort.Slice(keys,func(i,j int)bool{
  if len(keys[i])!=len(keys[j]){return len(keys[i])>len(keys[j])}
  return keys[i]<keys[j]
 })
 updated:=original
 for _,key:=range keys{updated=bytes.ReplaceAll(updated,[]byte(key),[]byte(replacements[key]))}
 if bytes.Equal(updated,original){return entry,nil}
 if !insideStaging(ir.StagingDir,source){
  if ir.StagingDir==""{return entry,fmt.Errorf("cannot stage external configuration without StagingDir")}
  target:=filepath.Join(ir.StagingDir,".mutate-cow",strings.TrimPrefix(dest,"/"))
  if err:=copyIntoStage(source,target);err!=nil{return entry,fmt.Errorf("copy configuration %q: %w",dest,err)}
  source=target
 }
 if err:=writeOwnedConfig(source,updated);err!=nil{return entry,err}
 entry.SourcePath=source
 return entry,nil
}

func isTextConfig(path string)bool{
 return strings.HasSuffix(path,".desktop")||
  strings.HasSuffix(path,".service")||
  strings.HasSuffix(path,".pc")
}

// NormalizePackage changes package destinations, never the caller's originals.
// It detects path collisions before touching staging files.
func NormalizePackage(ir *domain.PackageIR,targetDistro resolver.Distro)error{
 if ir==nil{return fmt.Errorf("nil package")}
 target:=GetTargetFHS(targetDistro,ir.Metadata.Architecture)
 newFiles:=make(map[string]domain.FileEntry,len(ir.Files))
 sourceDest:=make(map[string]string,len(ir.Files))
 replacements:=make(map[string]string)
 for dest,entry:=range ir.Files {
  newDest:=relocatedLibraryPath(dest,target.PrimaryLibDir)
  if prev,ok:=sourceDest[newDest];ok{
   return fmt.Errorf("FHS normalization collision at %s (%s and %s)",newDest,prev,dest)
  }
  sourceDest[newDest]=dest
  if newDest!=dest{
   replacements[dest]=newDest
   for _,prefix:=range []string{
    "/usr/lib64/","/usr/lib/x86_64-linux-gnu/","/usr/lib/aarch64-linux-gnu/",
   }{
    if strings.HasPrefix(dest,prefix){replacements[prefix]=target.PrimaryLibDir+"/";break}
   }
  }
  entry.DestinationPath=newDest
  newFiles[newDest]=entry
 }
 // Deterministic order prevents a file relocation from masking a later
 // collision and makes failures reproducible.
 keys:=make([]string,0,len(ir.Files))
 for dest:=range ir.Files{keys=append(keys,dest)}
 sort.Strings(keys)
 for _,dest:=range keys {
  newDest:=relocatedLibraryPath(dest,target.PrimaryLibDir)
  if newDest==dest{continue}
  entry:=newFiles[newDest]
  if !entry.IsSymlink&&!entry.IsDir{
   if ir.StagingDir==""{return fmt.Errorf("cannot relocate %q without staging directory",dest)}
   source:=stageSource(ir,entry,dest)
   if source==""{return fmt.Errorf("file %q has no source",dest)}
   newSource:=filepath.Join(ir.StagingDir,strings.TrimPrefix(newDest,"/"))
   if source!=newSource {
    if err:=os.MkdirAll(filepath.Dir(newSource),0755);err!=nil{return err}
    if _,err:=os.Lstat(newSource);err==nil{
     return fmt.Errorf("FHS staging path already exists: %q",newSource)
    }else if !os.IsNotExist(err){return err}
    if insideStaging(ir.StagingDir,source){
     if err:=os.Rename(source,newSource);err!=nil{return fmt.Errorf("relocate %q: %w",dest,err)}
    }else if err:=copyIntoStage(source,newSource);err!=nil{
     return fmt.Errorf("copy external source for %q: %w",dest,err)
    }
    entry.SourcePath=newSource
   }
  }
  newFiles[newDest]=entry
 }
 if len(replacements)>0{
  dests:=make([]string,0,len(newFiles))
  for dest:=range newFiles{dests=append(dests,dest)}
  sort.Strings(dests)
  for _,dest:=range dests{
   entry:=newFiles[dest]
   if entry.IsDir||entry.IsSymlink||!isTextConfig(dest){continue}
   updated,err:=updateConfigFilePaths(ir,entry,dest,replacements)
   if err!=nil{return fmt.Errorf("update config %s: %w",dest,err)}
   newFiles[dest]=updated
  }
 }
 ir.Files=newFiles
 return nil
}
