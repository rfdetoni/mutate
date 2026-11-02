package unpacker

import (
	"archive/tar"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/rfdetoni/mutate/internal/domain"
)

type BuildSystem string

const (
	BuildSystemGo        BuildSystem = "go"
	BuildSystemCargo     BuildSystem = "cargo"
	BuildSystemCMake     BuildSystem = "cmake"
	BuildSystemMeson     BuildSystem = "meson"
	BuildSystemAutotools BuildSystem = "autotools"
	BuildSystemMake      BuildSystem = "make"
	BuildSystemUnknown   BuildSystem = "unknown"
)

type SourceUnpacker struct{}

func NewSourceUnpacker() *SourceUnpacker {
	return &SourceUnpacker{}
}

func (u *SourceUnpacker) DetectBuildSystem(srcDir string) BuildSystem {
	if _, err := os.Stat(filepath.Join(srcDir, "go.mod")); err == nil {
		return BuildSystemGo
	}
	if _, err := os.Stat(filepath.Join(srcDir, "Cargo.toml")); err == nil {
		return BuildSystemCargo
	}
	if _, err := os.Stat(filepath.Join(srcDir, "CMakeLists.txt")); err == nil {
		return BuildSystemCMake
	}
	if _, err := os.Stat(filepath.Join(srcDir, "meson.build")); err == nil {
		return BuildSystemMeson
	}
	if _, err := os.Stat(filepath.Join(srcDir, "configure")); err == nil {
		return BuildSystemAutotools
	}
	if _, err := os.Stat(filepath.Join(srcDir, "Makefile")); err == nil {
		return BuildSystemMake
	}
	return BuildSystemUnknown
}

func (u *SourceUnpacker) Unpack(srcPath string, stagingDir string) (*domain.PackageIR, error) {
	// 1. Extract tarball into temporary source directory
	srcBuildDir := filepath.Join(stagingDir, ".src_build")
	if err := os.MkdirAll(srcBuildDir, 0755); err != nil {
		return nil, err
	}

	f, err := os.Open(srcPath)
	if err != nil {
		return nil, fmt.Errorf("open source archive: %w", err)
	}
	defer f.Close()

	comp := getTarCompression(srcPath)
	decomp, err := DecompressStream(f, comp)
	if err != nil {
		return nil, fmt.Errorf("decompress source: %w", err)
	}
	defer decomp.Close()

	tr := tar.NewReader(decomp)
	if err := ExtractTarStream(tr, srcBuildDir, nil); err != nil {
		return nil, fmt.Errorf("extract source tree: %w", err)
	}

	// If source was extracted into a single subdirectory, descend into it
	workDir := findRootWorkDir(srcBuildDir)

	buildSys := u.DetectBuildSystem(workDir)
	pkgName := derivePackageName(srcPath)

	ir := domain.NewPackageIR(stagingDir, domain.FormatTarball)
	ir.Metadata = domain.Metadata{
		Name:         pkgName,
		Version:      "1.0.0",
		Release:      "1",
		Architecture: runtime.GOARCH,
		Description:  fmt.Sprintf("Compiled package from source (%s)", buildSys),
		License:      "Unknown",
	}

	// Parsing and inspection never compile or execute untrusted source code.
	return ir, nil
}

// BuildSource requires explicit permission from the CLI. Source builds require bubblewrap isolation; inspect/dry-run never compile.
func (u *SourceUnpacker) BuildSource(ir *domain.PackageIR) error {
	srcDir := filepath.Join(ir.StagingDir, ".src_build")
	workDir := findRootWorkDir(srcDir)
	sys := u.DetectBuildSystem(workDir)
	if sys == BuildSystemUnknown {
		return fmt.Errorf("unsupported source build system")
	}
	if err := u.CompileAndInstall(workDir, ir.StagingDir, sys, ir.Metadata.Name); err != nil {
		return err
	}
	if err := os.RemoveAll(srcDir); err != nil {
		return err
	}
	return indexStagingFiles(ir.StagingDir, ir)
}

func (u *SourceUnpacker) CompileAndInstall(workDir, destDir string, buildSys BuildSystem, pkgName string) error {
 usrBin:=filepath.Join(destDir,"usr/bin")
 if err:=os.MkdirAll(usrBin,0755);err!=nil{return err}
 run:=func(args ...string)error{return runIsolatedBuild(workDir,destDir,args...)}
 switch buildSys {
 case BuildSystemGo:
  return run("go","build","-o",filepath.Join(usrBin,pkgName),".")
 case BuildSystemCargo:
  return run("cargo","install","--offline","--path",".","--root",filepath.Join(destDir,"usr"))
 case BuildSystemCMake:
  buildDir:=filepath.Join(workDir,"build")
  if err:=run("cmake","-B",buildDir,"-DCMAKE_INSTALL_PREFIX=/usr");err!=nil{return err}
  if err:=run("cmake","--build",buildDir);err!=nil{return err}
  return run("cmake","--install",buildDir,"--prefix",filepath.Join(destDir,"usr"))
 case BuildSystemMeson:
  buildDir:=filepath.Join(workDir,"build")
  if err:=run("meson","setup",buildDir,"--prefix=/usr");err!=nil{return err}
  if err:=run("meson","compile","-C",buildDir);err!=nil{return err}
  return run("meson","install","-C",buildDir)
 case BuildSystemAutotools:
  if err:=run("./configure","--prefix=/usr");err!=nil{return err}
  if err:=run("make");err!=nil{return err}
  return run("make","install","DESTDIR="+destDir)
 case BuildSystemMake:
  if err:=run("make");err!=nil{return err}
  return run("make","install","DESTDIR="+destDir,"PREFIX=/usr")
 default:
  return fmt.Errorf("unsupported source build system %q",buildSys)
 }
}

func findRootWorkDir(srcBuildDir string) string {
	entries, err := os.ReadDir(srcBuildDir)
	if err == nil && len(entries) == 1 && entries[0].IsDir() {
		return filepath.Join(srcBuildDir, entries[0].Name())
	}
	return srcBuildDir
}

func derivePackageName(path string) string {
	base := filepath.Base(path)
	for _, ext := range []string{".tar.gz", ".tar.xz", ".tar.zst", ".tgz", ".tar", ".zip"} {
		if strings.HasSuffix(base, ext) {
			base = strings.TrimSuffix(base, ext)
			break
		}
	}
	// Split by version dash if present e.g. myapp-1.2.3 -> myapp
	parts := strings.Split(base, "-")
	return strings.ToLower(parts[0])
}

func indexStagingFiles(stagingDir string, ir *domain.PackageIR) error {
	return filepath.Walk(stagingDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == stagingDir {
			return nil
		}
		if path == filepath.Join(stagingDir, ".src_build") {
			return filepath.SkipDir
		}

		rel, err := filepath.Rel(stagingDir, path)
		if err != nil {
			return err
		}

		destInHost := "/" + rel
		entry := domain.FileEntry{
			SourcePath:      path,
			DestinationPath: destInHost,
			Mode:            info.Mode(),
			ModTime:         info.ModTime(),
			IsDir:           info.IsDir(),
		}

		if info.Mode()&os.ModeSymlink != 0 {
			entry.IsSymlink = true
			if target, err := os.Readlink(path); err == nil {
				entry.SymlinkTarget = target
			}
		}

		ir.Files[destInHost] = entry
		return nil
	})
}
