package unpacker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runIsolatedBuild uses Linux user, mount, process and network namespaces.
// Missing bubblewrap or unavailable user namespaces fail closed. There is no
// unsafe fallback to running a source archive's scripts directly on the host.
func runIsolatedBuild(workDir, stage string, argv ...string) error {
	if len(argv) == 0 {
		return fmt.Errorf("empty source build command")
	}
	exe, err := exec.LookPath("bwrap")
	if err != nil {
		return fmt.Errorf("source builds require bubblewrap (bwrap) and user namespaces: %w", err)
	}
	absStage, err := filepath.Abs(stage)
	if err != nil {
		return err
	}
	absWork, err := filepath.Abs(workDir)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absStage, absWork)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("source build workdir must be inside private staging")
	}
	command := argv[0]
	if !strings.HasPrefix(command, "./") {
		command, err = exec.LookPath(command)
		if err != nil {
			return fmt.Errorf("required build tool %s: %w", argv[0], err)
		}
	}
	// The sandbox sees installed toolchains, but neither the user's home nor
	// arbitrary host files. Its only writable persistent mount is stage.
	args := []string{
		"--die-with-parent", "--new-session",
		"--unshare-user", "--unshare-pid", "--unshare-ipc", "--unshare-net", "--unshare-uts",
		"--ro-bind", "/usr", "/usr",
		"--ro-bind-try", "/bin", "/bin",
		"--ro-bind-try", "/lib", "/lib",
		"--ro-bind-try", "/lib64", "/lib64",
		"--dev", "/dev", "--proc", "/proc",
		"--tmpfs", "/tmp",
		"--bind", absStage, absStage,
		"--chdir", absWork, "--clearenv",
		"--setenv", "HOME", "/tmp",
		"--setenv", "TMPDIR", "/tmp",
		"--setenv", "PATH", "/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin",
		"--setenv", "GOPROXY", "off",
		"--setenv", "GOSUMDB", "off",
		"--setenv", "GOTOOLCHAIN", "local",
		"--setenv", "CARGO_NET_OFFLINE", "true",
		"--setenv", "DESTDIR", absStage,
	}
 // Optional explicit read-only cache, never the caller's home by default.
 // Cache layout: <dir>/go-mod and <dir>/cargo.
 if raw:=os.Getenv("MUTATE_BUILD_CACHE_DIR");raw!=""{
  cache,err:=filepath.EvalSymlinks(raw)
  if err!=nil{return fmt.Errorf("resolve build cache: %w",err)}
  cache,err=filepath.Abs(cache)
  if err!=nil{return err}
  st,err:=os.Stat(cache)
  if err!=nil{return err}
  if !st.IsDir(){return fmt.Errorf("build cache must be a directory")}
  args=append(args,"--ro-bind",cache,"/mutate-cache",
   "--setenv","GOMODCACHE","/mutate-cache/go-mod",
   "--setenv","CARGO_HOME","/mutate-cache/cargo")
 }
 args=append(args,"--",command)
 args = append(args, argv[1:]...)
	cmd := exec.Command(exe, args...)
	cmd.Dir = absWork
	// Avoid inheriting secrets through the environment.
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=/tmp"}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sandboxed %s: %w: %s", argv[0], err, string(output))
	}
	return nil
}
