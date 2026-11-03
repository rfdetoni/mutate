package main

import (
 "context"
 "io"
 "encoding/json"
 "fmt"
 "net/http"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "time"

 "github.com/rfdetoni/mutate/internal/domain"
 "github.com/rfdetoni/mutate/internal/elfanalyzer"
 "github.com/rfdetoni/mutate/internal/packager"
 "github.com/rfdetoni/mutate/internal/resolver"
 "github.com/rfdetoni/mutate/internal/unpacker"
)

type guiActionRequest struct{
 Action string `json:"action"`
 Target string `json:"target"`
 Resolve bool `json:"resolve"`
 Extract bool `json:"extract"`
 AllowSourceBuild bool `json:"allow_source_build"`
 DependencyMap string `json:"dependency_map"`
}

func(a *guiApp)handleAction(w http.ResponseWriter,r *http.Request){
 r.Body=http.MaxBytesReader(w,r.Body,128<<10)
 decoder:=json.NewDecoder(r.Body)
 decoder.DisallowUnknownFields()
 var req guiActionRequest
 if err:=decoder.Decode(&req);err!=nil{
  guiError(w,400,"Opções inválidas: "+err.Error());return
 }
 if req.Action!="inspect"&&req.Action!="convert"&&req.Action!="install"{
  guiError(w,400,"Operação desconhecida");return
 }
 if req.Action=="convert" {
  if _,_,err:=distroForFormat(req.Target);err!=nil{
   guiError(w,400,err.Error());return
  }
 }
 if req.Action=="install"{
  req.Target=nativeGUITarget(resolver.DetectHostDistro())
  if req.Target==""{guiError(w,422,"Distribuição não suportada para instalação automática");return}
 }
 if strings.TrimSpace(req.DependencyMap)!="" {
  if _,err:=readDependencyMap(req.DependencyMap);err!=nil {
   guiError(w,400,"Mapeamento de dependências inválido: "+err.Error());return
  }
 }
 a.mu.Lock()
 if a.busy{a.mu.Unlock();guiError(w,409,"Já existe uma operação em andamento");return}
 if a.selected==nil{a.mu.Unlock();guiError(w,409,"Selecione um pacote antes de continuar");return}
 selected:=*a.selected
 id,err:=newSessionToken()
 if err!=nil{a.mu.Unlock();guiError(w,500,"Não foi possível iniciar a operação");return}
 job:=&guiJob{ID:id,Operation:req.Action,State:"running",Phase:"Preparando",StartedAt:time.Now()}
 a.job=job
 a.busy=true
 a.mu.Unlock()
 go a.runAction(selected,req,id)
 guiJSON(w,202,map[string]string{"job_id":id})
}

func (a *guiApp)phase(id,phase string){
 a.mu.Lock()
 defer a.mu.Unlock()
 if a.job!=nil&&a.job.ID==id{a.job.Phase=phase}
}

func (a *guiApp)runAction(selected guiPackage,req guiActionRequest,id string) {
 var result guiJob
 result.ID=id
 result.Operation=req.Action
 result.StartedAt=time.Now()
 var err error
 switch req.Action{
 case "inspect":
  err=a.inspectDeep(selected,req,id,&result)
 case "convert","install":
  err=a.convertAndMaybeInstall(selected,req,id,&result)
 }
 a.mu.Lock()
 defer a.mu.Unlock()
 if a.job==nil||a.job.ID!=id{return}
 a.job.State="completed"
 if err!=nil{
  a.job.State="error"
  a.job.Message=err.Error()
  a.job.Phase="Não concluído"
 }else{
  a.job.Message=result.Message
  a.job.Phase="Concluído"
 }
 a.job.ExternalLibraries=result.ExternalLibraries
 a.job.ELFs=result.ELFs
 a.job.OutputID=result.OutputID
 a.job.OutputName=result.OutputName
 a.busy=false
 entry:=guiHistory{
  Operation:req.Action,Name:selected.Name,Target:req.Target,
  State:a.job.State,OutputID:a.job.OutputID,OutputName:a.job.OutputName,
  At:time.Now(),
 }
 a.history=append([]guiHistory{entry},a.history...)
 if len(a.history)>20{a.history=a.history[:20]}
}

func(a *guiApp)inspectDeep(selected guiPackage,req guiActionRequest,id string,result *guiJob)error{
 stage,err:=os.MkdirTemp(a.workspace,"inspect-*")
 if err!=nil{return err}
 defer os.RemoveAll(stage)
 a.phase(id,"Lendo conteúdo do pacote")
 ir,err:=unpacker.Prepare(selected.Path,selected.Format,stage,unpacker.PrepareOptions{Extract:req.Extract})
 if err!=nil{return fmt.Errorf("inspeção: %w",err)}
 a.phase(id,"Analisando executáveis ELF")
 analysis,err:=elfanalyzer.AnalyzePackage(ir)
 if err!=nil{return fmt.Errorf("análise ELF: %w",err)}
 result.ELFs=analysis.TotalELFs
 result.ExternalLibraries=append([]string{},analysis.ExternalLibraries...)
 if req.Resolve&&len(analysis.ExternalLibraries)>0{
  a.phase(id,"Consultando bibliotecas do sistema")
  if provider,err:=resolver.NewResolverFor(resolver.DetectHostDistro());err==nil{
   resolved:=resolver.ResolveLibraries(provider,analysis.ExternalLibraries)
   count:=0
   for _,lib:=range analysis.ExternalLibraries{if resolved[lib]!=""{count++}}
   result.Message=fmt.Sprintf("%d ELF; %d de %d bibliotecas atendidas pelo host",analysis.TotalELFs,count,len(analysis.ExternalLibraries))
  }
 }
 if result.Message==""{
  result.Message=fmt.Sprintf("%d binários ELF analisados; %d bibliotecas externas",analysis.TotalELFs,len(analysis.ExternalLibraries))
 }
 return nil
}

func nativeGUITarget(host resolver.Distro)string{
 switch host{
 case resolver.DistroDebian:return "deb"
 case resolver.DistroFedora:return "rpm"
 case resolver.DistroArch:return "arch"
 case resolver.DistroAlpine:return "apk"
 }
 return ""
}

func guiExtension(target string)string{
 if target=="arch"{return "pkg.tar.zst"}
 return target
}
func guiOutputName(ir *domain.PackageIR,target string)string{
 raw:=fmt.Sprintf("%s_%s-%s_%s.%s",ir.Metadata.Name,ir.Metadata.Version,ir.Metadata.Release,ir.Metadata.Architecture,guiExtension(target))
 return cleanUploadName(raw)
}

func(a *guiApp)convertAndMaybeInstall(selected guiPackage,req guiActionRequest,id string,result *guiJob)error{
 a.phase(id,"Verificando compatibilidade e dependências")
 ir,cleanup,err:=prepareConversionWithOptions(selected.Path,req.Target,req.AllowSourceBuild,conversionOptions{
  DependencyMapPath:req.DependencyMap,Extract:req.Extract,
 })
 if err!=nil{return fmt.Errorf("conversão: %w",err)}
 defer cleanup()
 a.phase(id,"Gerando pacote "+strings.ToUpper(req.Target))
 outputID,err:=newSessionToken()
 if err!=nil{return err}
 outputsDir:=filepath.Join(a.workspace,"outputs")
 if err:=os.MkdirAll(outputsDir,0700);err!=nil{return err}
 outputName:=guiOutputName(ir,req.Target)
 if outputName==""{return fmt.Errorf("nome de pacote de saída inválido")}
 outputPath:=filepath.Join(outputsDir,outputID+"."+guiExtension(req.Target))
 if err:=packager.BuildPackageToFile(ir,req.Target,outputPath);err!=nil{return fmt.Errorf("empacotamento: %w",err)}
 a.mu.Lock()
 a.outputs[outputID]=guiOutput{Path:outputPath,Name:outputName}
 a.mu.Unlock()
 result.OutputID=outputID
 result.OutputName=outputName
 if req.Action=="install"{
  a.phase(id,"Solicitando autorização do sistema")
  if err:=installGUIOutput(outputPath,resolver.DetectHostDistro());err!=nil{
   return fmt.Errorf("instalação falhou: %w. O pacote convertido permanece disponível para download",err)
  }
  result.Message="Pacote instalado com autorização do sistema"
 }else{
  result.Message="Pacote convertido e pronto para baixar"
 }
 return nil
}

func installGUIOutput(path string,host resolver.Distro)error{
 var executable string
 var args []string
 switch host{
 case resolver.DistroDebian:executable="apt-get";args=[]string{"install","-y",path}
 case resolver.DistroFedora:executable="dnf";args=[]string{"install","-y",path}
 case resolver.DistroArch:executable="pacman";args=[]string{"-U","--noconfirm",path}
 case resolver.DistroAlpine:executable="apk";args=[]string{"add","--allow-untrusted",path}
 default:return fmt.Errorf("distribuição não suportada")
 }
 if os.Geteuid()!=0 {
  if _,err:=exec.LookPath("pkexec");err!=nil{
   return fmt.Errorf("pkexec (PolicyKit) não encontrado; instale o pacote manualmente com privilégios de administrador")
  }
  args=append([]string{executable},args...)
  executable="pkexec"
 }
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Minute)
 defer cancel()
 // pkexec must receive the resolved executable path, not an
 // arbitrary command name or shell expression.
 if executable=="pkexec"{
  manager,err:=exec.LookPath(args[0])
  if err!=nil{return fmt.Errorf("gerenciador de pacotes indisponível: %w",err)}
  args[0]=manager
 }
 command:=exec.CommandContext(ctx,executable,args...)
 var stderr cappedGUIBuffer
 command.Stdout=io.Discard
 command.Stderr=&stderr
 if err:=command.Run();err!=nil{
  if ctx.Err()!=nil{return fmt.Errorf("tempo limite da instalação excedido")}
  message:=strings.TrimSpace(stderr.String())
  if len(message)>1500{message=message[len(message)-1500:]}
  if message!=""{return fmt.Errorf("%s: %w",message,err)}
  return err
 }
 return nil
}

// Keep only the tail of native package-manager diagnostics.
type cappedGUIBuffer struct{data []byte}
func(b *cappedGUIBuffer)Write(p []byte)(int,error){
 b.data=append(b.data,p...)
 if len(b.data)>4096{b.data=append([]byte(nil),b.data[len(b.data)-4096:]...)}
 return len(p),nil
}
func(b *cappedGUIBuffer)String()string{return string(b.data)}
