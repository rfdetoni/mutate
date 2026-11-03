package prerequisites

import (
 "bytes"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"

 "github.com/rfdetoni/mutate/internal/domain"
 "github.com/rfdetoni/mutate/internal/resolver"
)

// HostState describes observable prerequisites on the machine running mutate.
// These probes do not execute a package or start an emulator.
type HostState struct {
 WaydroidExecutable bool
 FuseDevice bool
 LegacyFuseLibrary bool
}

type Notice struct {
 Warning bool
 Message string
}

func DetectHostState() HostState {
 _,waydroidErr:=exec.LookPath("waydroid")
 _,fuseErr:=os.Stat("/dev/fuse")
 return HostState{
  WaydroidExecutable:waydroidErr==nil,
  FuseDevice:fuseErr==nil,
  LegacyFuseLibrary:hasLegacyFuseLibrary(),
 }
}

func hasLegacyFuseLibrary() bool {
 patterns:=[]string{
  "/lib/libfuse.so.2","/usr/lib/libfuse.so.2",
  "/lib64/libfuse.so.2","/usr/lib64/libfuse.so.2",
  "/lib/*-linux-gnu/libfuse.so.2","/usr/lib/*-linux-gnu/libfuse.so.2",
 }
 for _,pattern:=range patterns{
  paths,err:=filepath.Glob(pattern)
  if err!=nil{continue}
  for _,path:=range paths{
   if st,err:=os.Stat(path);err==nil && st.Mode().IsRegular(){return true}
  }
 }
 return false
}

func fuseInstallHint(distro resolver.Distro) string {
 switch distro {
 case resolver.DistroDebian:
  return "Debian/Ubuntu: install libfuse2t64 (newer systems) or libfuse2 (older systems); do not replace fuse3 with the legacy fuse package."
 case resolver.DistroArch:
  return "Arch: sudo pacman -S fuse2"
 case resolver.DistroFedora:
  return "Fedora: sudo dnf install fuse fuse-libs"
 case resolver.DistroAlpine:
  return "Alpine: check availability of the FUSE 2 runtime libraries in your release."
 default:
  return "See https://docs.appimage.org/user-guide/troubleshooting/fuse.html"
 }
}

// Check returns only advisory messages; missing runtime prerequisites must not
// prevent generating a package for another machine. The local host is not
// necessarily the host where the package will be installed and executed.
func Check(format domain.Format, source string, extract bool, host HostState, distro resolver.Distro) ([]Notice,error) {
 switch format {
 case domain.FormatAndroidAPK:
  if !host.WaydroidExecutable{
   return []Notice{{Warning:true,Message:"Android APK requires Waydroid on the system where it will run. The 'waydroid' command was not found on this host. Install and configure Waydroid: https://docs.waydro.id/usage/install-on-desktops"}},nil
  }
  return []Notice{{Message:"Waydroid executable found on this host. Verify that its container/session is configured and running before launching the APK."}},nil
 case domain.FormatAppImage:
  f,err:=os.Open(source)
  if err!=nil{return nil,fmt.Errorf("read AppImage runtime marker: %w",err)}
  defer f.Close()
  var marker [3]byte
  if _,err:=f.ReadAt(marker[:],8);err!=nil{return nil,fmt.Errorf("read AppImage runtime marker: %w",err)}
  if !bytes.Equal(marker[:],[]byte{'A','I',2}){
   // Legacy ISO9660 Type 1 (also before the AI 01 marker). It does not
   // share Type 2's usual libfuse2 runtime dependency.
   return []Notice{{Message:"AppImage Type 1 (ISO9660): FUSE 2 is not generally required; direct execution may need ISO mounting support and permissions on the target host."}},nil
  }
  if extract {
   return []Notice{{Message:"AppImage Type 2 will be extracted: this conversion path does not require FUSE to mount the original runtime."}},nil
  }
  notices:=[]Notice{{Message:"AppImage Type 2 preserved: direct execution may require FUSE. Some older runtimes need libfuse.so.2; newer runtimes may provide FUSE 3 support."}}
  if !host.FuseDevice {
   notices=append(notices,Notice{Warning:true,Message:"/dev/fuse is unavailable on this host. Configure FUSE on the machine that will run the AppImage. "+fuseInstallHint(distro)})
  }
  if !host.LegacyFuseLibrary {
   notices=append(notices,Notice{Warning:true,Message:"libfuse.so.2 was not found on this host. Older AppImage Type 2 runtimes may require FUSE 2. "+fuseInstallHint(distro)})
  }
  return notices,nil
 default:
  return nil,nil
 }
}
