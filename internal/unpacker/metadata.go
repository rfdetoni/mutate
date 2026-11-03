package unpacker

import (
 "archive/tar"
 "bufio"
 "compress/gzip"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "strings"

 "github.com/blakesmith/ar"
 "github.com/cavaliergopher/rpm"
 "github.com/rfdetoni/mutate/internal/domain"
)

// InspectMetadata reads package headers without materializing the payload.
// It deliberately does not infer binary ABI compatibility or execute scripts.
func InspectMetadata(src string,format domain.Format)(domain.Metadata,error){
 f,err:=os.Open(src)
 if err!=nil{return domain.Metadata{},err}
 defer f.Close()
 switch format {
 case domain.FormatDEB:
  arReader:=ar.NewReader(f)
  for{
   header,err:=arReader.Next()
   if err==io.EOF{break}
   if err!=nil{return domain.Metadata{},err}
   if !strings.HasPrefix(strings.TrimSpace(header.Name),"control.tar"){continue}
   decomp,err:=DecompressStream(arReader,getTarCompression(header.Name))
   if err!=nil{return domain.Metadata{},err}
   ir:=domain.NewPackageIR("",domain.FormatDEB)
   err=NewDebUnpacker().parseControlTar(decomp,ir)
   closeErr:=decomp.Close()
   if err!=nil{return domain.Metadata{},err}
   if closeErr!=nil{return domain.Metadata{},closeErr}
   if ir.Metadata.Name!=""{return ir.Metadata,nil}
   return domain.Metadata{},fmt.Errorf("missing DEB control metadata")
  }
 case domain.FormatRPM:
  header,err:=rpm.Read(f)
  if err!=nil{return domain.Metadata{},err}
  m:=domain.Metadata{
   Name:header.Name(),Version:header.Version(),Release:header.Release(),
   Architecture:header.Architecture(),Description:header.Description(),
   License:header.License(),Vendor:header.Vendor(),Homepage:header.URL(),
  }
  for _,req:=range header.Requires(){
   if name:=req.Name();!strings.HasPrefix(name,"rpmlib("){m.Dependencies=append(m.Dependencies,name)}
  }
  for _,prov:=range header.Provides(){m.Provides=append(m.Provides,prov.Name())}
  return m,nil
 case domain.FormatArch:
  compression:="zst"
  if strings.HasSuffix(src,".xz"){compression="xz"}else if strings.HasSuffix(src,".gz"){compression="gz"}
  dec,err:=DecompressStream(f,compression)
  if err!=nil{return domain.Metadata{},err}
  defer dec.Close()
  tr:=tar.NewReader(dec)
  for {
   hdr,err:=tr.Next()
   if err==io.EOF{break}
   if err!=nil{return domain.Metadata{},err}
   if strings.TrimPrefix(filepath.Clean(hdr.Name),"./")==".PKGINFO" {
    raw,err:=readMetadata(tr)
    if err!=nil{return domain.Metadata{},err}
    var m domain.Metadata
    parseArchPkgInfo(raw,&m)
    if m.Name==""{return domain.Metadata{},fmt.Errorf("missing Arch package name")}
    return m,nil
   }
  }
 case domain.FormatAlpineAPK:
  br:=bufio.NewReader(f)
  for{
   if _,err:=br.Peek(1);err==io.EOF{break}else if err!=nil{return domain.Metadata{},err}
   gz,err:=gzip.NewReader(br)
   if err!=nil{return domain.Metadata{},err}
   gz.Multistream(false)
   tr:=tar.NewReader(gz)
   for{
    hdr,err:=tr.Next()
    if err==io.EOF{break}
    if err!=nil{_ = gz.Close();return domain.Metadata{},err}
    if strings.TrimPrefix(filepath.Clean(hdr.Name),"./")==".PKGINFO"{
     raw,err:=readMetadata(tr)
     _=gz.Close()
     if err!=nil{return domain.Metadata{},err}
     var m domain.Metadata
     parseAlpinePkgInfo(raw,&m)
     if m.Name==""{return domain.Metadata{},fmt.Errorf("missing Alpine package name")}
     return m,nil
    }
   }
   if _,err:=io.Copy(io.Discard,gz);err!=nil{_ = gz.Close();return domain.Metadata{},err}
   if err:=gz.Close();err!=nil{return domain.Metadata{},err}
  }
 case domain.FormatTarball:
  return domain.Metadata{
   Name:derivePackageName(src),Version:"1.0.0",Release:"1",
   Architecture:"source",Description:"Source tarball (build requires --allow-source-build)",
  },nil
 default:
  return domain.Metadata{},fmt.Errorf("fast metadata inspection unsupported for format %s",format)
 }
 return domain.Metadata{},fmt.Errorf("required %s metadata not found",format)
}
