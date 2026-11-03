// mutate-gui is the optional native Wails 3 shell. The CLI remains CGO-free.
// The shell only loads the authenticated loopback session created by mutate gui.
package main

import (
 _ "embed"
 "fmt"
 "net"
 "net/url"
 "os"
 "strings"

 "github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed icon.png
var icon []byte

func validateLocalURL(raw string) error {
 u,err:=url.Parse(raw)
 if err!=nil||u.Scheme!="http"||u.User!=nil||u.RawQuery!=""||u.Fragment!=""{
  return fmt.Errorf("endereço de sessão inválido")
 }
 if u.Hostname()!="127.0.0.1"||u.Port()==""{
  return fmt.Errorf("a interface aceita somente sessões locais 127.0.0.1")
 }
 if _,_,err=net.SplitHostPort(u.Host);err!=nil{return err}
 if !strings.HasPrefix(u.Path,"/")||!strings.HasSuffix(u.Path,"/")||len(u.Path)<16{
  return fmt.Errorf("caminho de sessão inválido")
 }
 return nil
}

func main(){
 if len(os.Args)!=2{
  fmt.Fprintln(os.Stderr,"Uso: mutate-gui http://127.0.0.1:<porta>/<sessao>/")
  os.Exit(2)
 }
 raw:=os.Args[1]
 if err:=validateLocalURL(raw);err!=nil{
  fmt.Fprintln(os.Stderr,err)
  os.Exit(2)
 }
 app:=application.New(application.Options{Name:"M.U.T.A.T.E."})
 window:=app.Window.NewWithOptions(application.WebviewWindowOptions{
  Name:"main", Title:"M.U.T.A.T.E.",URL:raw,
  Width:1180,Height:780,MinWidth:800,MinHeight:600,
  BackgroundColour:application.NewRGB(248,249,250),
  Linux:application.LinuxWindow{Icon:icon},
 })
 window.Center()
 window.Show()
 if err:=app.Run();err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}
}
