package unpacker

import (
 "fmt"
 "github.com/rfdetoni/mutate/internal/domain"
)

// PrepareOptions defines the same preserve/extract policy for all CLI paths.
type PrepareOptions struct {
 Extract bool
 PackageName string
 PackageVersion string
}

// NewForFormat is the single mapping between known formats and handlers.
func NewForFormat(format domain.Format)(Unpacker,error){
 switch format {
 case domain.FormatDEB:return NewDebUnpacker(),nil
 case domain.FormatRPM:return NewRpmUnpacker(),nil
 case domain.FormatArch:return NewArchUnpacker(),nil
 case domain.FormatAlpineAPK:return NewAlpineUnpacker(),nil
 case domain.FormatAndroidAPK:return NewAndroidUnpacker(),nil
 case domain.FormatTarball:return NewSourceUnpacker(),nil
 case domain.FormatAppImage:return NewAppImageUnpacker(),nil
 default:return nil,fmt.Errorf("unsupported package format: %s",format)
 }
}

// Prepare centralizes the preserve/extract decision. AppImage overrides are
// validated before extracted paths and launchers are constructed.
func Prepare(src string, format domain.Format,stage string,opts PrepareOptions)(*domain.PackageIR,error){
 if (opts.PackageName!=""||opts.PackageVersion!="")&&format!=domain.FormatAppImage{
  return nil,fmt.Errorf("--package-name and --package-version only apply to AppImage")
 }
 if opts.Extract&&format!=domain.FormatAppImage{
  return nil,fmt.Errorf("--extract is only applicable to AppImage; other formats use their normal conversion path")
 }
 u,err:=NewForFormat(format)
 if err!=nil{return nil,err}
 if app,ok:=u.(*AppImageUnpacker);ok {
  if opts.Extract{return app.UnpackWithMetadata(src,stage,opts.PackageName,opts.PackageVersion)}
  return app.PreserveWithMetadata(src,stage,opts.PackageName,opts.PackageVersion)
 }
 if preserver,ok:=u.(PortablePreserver);ok{return preserver.Preserve(src,stage)}
 return u.Unpack(src,stage)
}
