package domain

import (
 "fmt"
 "path/filepath"
 "strings"
)

// ValidatePackageIR enforces invariants at the final packaging boundary,
// regardless of which input format produced the intermediate representation.
// This intentionally does not restrict SourcePath to staging: portable
// AppImages/APKs legitimately reference immutable original files.
func ValidatePackageIR(ir *PackageIR)error{
 if ir==nil{return fmt.Errorf("nil package representation")}
 for destination,entry:=range ir.Files{
  if destination=="/"||!filepath.IsAbs(destination)||filepath.Clean(destination)!=destination||
   strings.ContainsAny(destination,"\x00\n\r"){
   return fmt.Errorf("invalid package destination %q",destination)
  }
  if entry.DestinationPath!=""&&entry.DestinationPath!=destination{
   return fmt.Errorf("destination mismatch %q != %q",entry.DestinationPath,destination)
  }
  if entry.IsDir&&entry.IsSymlink{
   return fmt.Errorf("entry %q cannot be both directory and symlink",destination)
  }
  if entry.IsSymlink&&entry.SymlinkTarget==""{
   return fmt.Errorf("symlink %q has no target",destination)
  }
  if !entry.IsDir&&!entry.IsSymlink&&entry.SourcePath==""&&ir.StagingDir==""{
   return fmt.Errorf("regular file %q has no source",destination)
  }
 }
 return nil
}
