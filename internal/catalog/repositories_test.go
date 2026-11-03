package catalog

import (
 "bytes"
 "context"
 "io"
 "net/http"
 "path/filepath"
 "strings"
 "sync"
 "testing"
 "time"

 "github.com/rfdetoni/mutate/internal/resolver"
)

type mockedTransport func(*http.Request)(*http.Response,error)
func (m mockedTransport)RoundTrip(r *http.Request)(*http.Response,error){return m(r)}
func mockedCatalogClient(fn func(*http.Request)(string,error))*http.Client{
 return &http.Client{Transport:mockedTransport(func(req *http.Request)(*http.Response,error){
  s,err:=fn(req)
  if err!=nil{return nil,err}
  return &http.Response{
   StatusCode:200,Status:"200 OK",Header:make(http.Header),Body:io.NopCloser(strings.NewReader(s)),Request:req,
  },nil
 })}
}

func TestRepositoryManagerDefaultsAndPersistence(t *testing.T){
 path:=filepath.Join(t.TempDir(),"settings","repositories.json")
 m,err:=NewRepositoryManager(path,nil)
 if err!=nil{t.Fatal(err)}
 sources:=m.Sources()
 if len(sources)!=3{t.Fatalf("expected 3 default remote sources, got %d",len(sources))}
 for _,s:=range sources{if !s.Enabled||!s.Builtin{t.Fatalf("invalid builtin %v",s)}}
 if err:=m.SetEnabled("aur",false);err!=nil{t.Fatal(err)}
 added,err:=m.Add("Other apps","https://packages.example.org/search?q={query}")
 if err!=nil{t.Fatal(err)}
 if added.ID==""||added.Kind!="json"{t.Fatalf("invalid added repo: %+v",added)}
 m.Close()
 again,err:=NewRepositoryManager(path,nil)
 if err!=nil{t.Fatal(err)}
 defer again.Close()
 if len(again.Sources())!=4{t.Fatalf("sources not persisted: %+v",again.Sources())}
 for _,s:=range again.Sources(){
  if s.ID=="aur"&&s.Enabled{t.Fatal("disabled builtin was re-enabled")}
 }
 if err:=again.Remove("arch");err==nil{t.Fatal("removed required built-in")}
 if err:=again.Remove(added.ID);err!=nil{t.Fatal(err)}
 if len(again.Sources())!=3{t.Fatal("custom source not removed")}
}

func TestRepositoryManagerRejectsUntrustedEndpoints(t *testing.T){
 path:=filepath.Join(t.TempDir(),"repositories.json")
 m,err:=NewRepositoryManager(path,nil)
 if err!=nil{t.Fatal(err)}
 defer m.Close()
 invalid:=[]string{
  "http://packages.example.org/search?q={query}",
  "https://localhost/search?q={query}",
  "https://127.0.0.1/search?q={query}",
  "https://10.0.0.9/search?q={query}",
  "https://user:password@packages.example.org/search?q={query}",
  "https://packages.example.org/search",
  "file:///etc/passwd?q={query}",
 }
 for _,endpoint:=range invalid{
  if _,err:=m.Add("Invalid",endpoint);err==nil{t.Fatalf("accepted dangerous endpoint %q",endpoint)}
 }
}

func TestRepositoryRefreshRunsInParallelAndCaches(t *testing.T){
 started:=make(chan string,3)
 release:=make(chan struct{})
 var mu sync.Mutex
 requests:=[]string{}
 client:=mockedCatalogClient(func(r *http.Request)(string,error){
  mu.Lock(); requests=append(requests,r.URL.Host);mu.Unlock()
  started<-r.URL.Host
  <-release
  switch r.URL.Host{
  case "aur.archlinux.org":
   return `{"type":"search","results":[{"Name":"firefox-nightly","Version":"12-1","Description":"Nightly"}]}`,nil
  case "archlinux.org":
   return `{"results":[{"pkgname":"firefox","pkgver":"1.0","pkgrel":"2","repo":"Extra","pkgdesc":"Firefox","arch":"x86_64"}]}`,nil
  case "sources.debian.org":
   return `{"results":{"exact":{"name":"firefox"},"other":[{"name":"firefox-esr"}]}}`,nil
  }
  return "",nil
 })
 m,err:=NewRepositoryManager(filepath.Join(t.TempDir(),"repos.json"),client)
 if err!=nil{t.Fatal(err)}
 defer m.Close()
 done:=make(chan struct{})
 go func(){m.Refresh(context.Background());close(done)}()
 for i:=0;i<3;i++{
  select {
  case <-started:
  case <-time.After(2*time.Second):close(release);t.Fatal("remote refresh did not start sources concurrently")
  }
 }
 close(release)
 select{case <-done:case <-time.After(2*time.Second):t.Fatal("refresh never completed")}
 for _,source:=range m.Sources(){if source.Status!="ok"{t.Fatalf("source %q failed: %+v",source.ID,source)}}
 m.mu.RLock()
 if len(m.cache)!=3{t.Fatalf("expected cache for 3 catalogs: %+v",m.cache)}
 m.mu.RUnlock()
 mu.Lock();defer mu.Unlock()
 if len(requests)!=3{t.Fatalf("unexpected requests: %+v",requests)}
}

func TestRepositorySearchKeepsRemoteResultsWhenNativeUnavailable(t *testing.T){
 client:=mockedCatalogClient(func(r *http.Request)(string,error){
  switch r.URL.Host{
  case "aur.archlinux.org":return `{"type":"search","results":[{"Name":"blender-git","Version":"4.5.0-1","Description":"Build"}]}`,nil
  case "archlinux.org":return `{"results":[{"pkgname":"blender","pkgver":"4.5.3","pkgrel":"2","repo":"Extra","pkgdesc":"3D tools","arch":"x86_64"}]}`,nil
  case "sources.debian.org":return `{"results":{"exact":{"name":"blender"},"other":[]}}`,nil
  }
  return "",nil
 })
 m,err:=NewRepositoryManager(filepath.Join(t.TempDir(),"repos.json"),client)
 if err!=nil{t.Fatal(err)}
 defer m.Close()
 out,err:=m.Search(context.Background(),resolver.DistroOther,"blender")
 if err!=nil{t.Fatal(err)}
 if len(out.Apps)!=3{t.Fatalf("remote results dropped when native unavailable: %+v",out)}
 for _,app:=range out.Apps{
  if app.Native||app.Installable{t.Fatalf("remote listing was marked installable: %+v",app)}
  if app.URL==""{t.Fatalf("missing origin URL: %+v",app)}
 }
 if len(out.Warnings)!=1{t.Fatalf("expected native source warning: %+v",out.Warnings)}
}

func TestCustomJSONCatalogSanitizesLinks(t *testing.T){
 client:=mockedCatalogClient(func(r *http.Request)(string,error){
  if r.URL.Query().Get("q")!="firefox"{t.Fatalf("wrong query: %s",r.URL)}
  return `{"results":[{"name":"firefox","version":"1.0","description":"Browser","url":"http://unsafe.test/"},{"name":"bad/name","version":"2","url":"https://trusted.example.org"}]}`,nil
 })
 m,err:=NewRepositoryManager(filepath.Join(t.TempDir(),"repos.json"),client)
 if err!=nil{t.Fatal(err)}
 defer m.Close()
 app,err:=m.searchOne(context.Background(),Source{ID:"custom",Name:"Custom",Kind:"json",Endpoint:"https://packages.example.org/search?q={query}",Enabled:true},"firefox")
 if err!=nil{t.Fatal(err)}
 if len(app)!=1||app[0].URL!=""{t.Fatalf("untrusted URL leaked: %+v",app)}
}

func TestRepositoryRequestBodyBounded(t *testing.T){
 client:=mockedCatalogClient(func(r *http.Request)(string,error){
  return string(bytes.Repeat([]byte("x"),2<<20+1)),nil
 })
 m,err:=NewRepositoryManager(filepath.Join(t.TempDir(),"repos.json"),client)
 if err!=nil{t.Fatal(err)}
 defer m.Close()
 if _,err:=m.searchOne(context.Background(),m.Sources()[1],"firefox");err==nil{t.Fatal("invalid oversized JSON should fail")}
}
