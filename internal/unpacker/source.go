package unpacker

import (
	"archive/tar"
	"fmt"
	"os"
	"os/exec"
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

// BuildSource requires explicit permission from the CLI. Source build scripts
// execute on the host; callers must not invoke this for inspect or dry-run.
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
	prefix := "/usr"
	usrBin := filepath.Join(destDir, "usr/bin")
	if err := os.MkdirAll(usrBin, 0755); err != nil {
		return err
	}

	switch buildSys {
	case BuildSystemGo:
		outBin := filepath.Join(usrBin, pkgName)
		cmd := exec.Command("go", "build", "-o", outBin, ".")
		cmd.Dir = workDir
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("go build failed: %s: %w", string(out), err)
		}

	case BuildSystemCargo:
		cmd := exec.Command("cargo", "install", "--path", ".", "--root", filepath.Join(destDir, "usr"))
		cmd.Dir = workDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("cargo install failed: %s: %w", string(out), err)
		}

	case BuildSystemCMake:
		buildDir := filepath.Join(workDir, "build")
		cmd1 := exec.Command("cmake", "-B", buildDir, "-DCMAKE_INSTALL_PREFIX="+prefix)
		cmd1.Dir = workDir
		if out, err := cmd1.CombinedOutput(); err != nil {
			return fmt.Errorf("cmake configure failed: %s: %w", string(out), err)
		}
		cmd2 := exec.Command("cmake", "--build", buildDir)
		cmd2.Dir = workDir
		if out, err := cmd2.CombinedOutput(); err != nil {
			return fmt.Errorf("cmake build failed: %s: %w", string(out), err)
		}
		cmd3 := exec.Command("cmake", "--install", buildDir, "--prefix", filepath.Join(destDir, "usr"))
		cmd3.Dir = workDir
		if out, err := cmd3.CombinedOutput(); err != nil {
			return fmt.Errorf("cmake install failed: %s: %w", string(out), err)
		}

	case BuildSystemMeson:
		buildDir := filepath.Join(workDir, "build")
		cmds := [][]string{{"meson", "setup", buildDir, "--prefix=/usr"}, {"meson", "compile", "-C", buildDir}, {"meson", "install", "-C", buildDir}}
		for _, argv := range cmds {
			cmd := exec.Command(argv[0], argv[1:]...)
			cmd.Dir = workDir
			cmd.Env = append(os.Environ(), "DESTDIR="+destDir)
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("%s failed: %s: %w", argv[0], out, err)
			}
		}
	case BuildSystemAutotools:
		cmds := [][]string{{"./configure", "--prefix=/usr"}, {"make"}, {"make", "install", "DESTDIR=" + destDir}}
		for _, argv := range cmds {
			cmd := exec.Command(argv[0], argv[1:]...)
			cmd.Dir = workDir
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("%s failed: %s: %w", argv[0], out, err)
			}
		}

	case BuildSystemMake:
		cmd1 := exec.Command("make")
		cmd1.Dir = workDir
		if out, err := cmd1.CombinedOutput(); err != nil {
			return fmt.Errorf("make failed: %s: %w", string(out), err)
		}
		cmd2 := exec.Command("make", "install", "DESTDIR="+destDir, "PREFIX="+prefix)
		cmd2.Dir = workDir
		if out, err := cmd2.CombinedOutput(); err != nil {
			return fmt.Errorf("make install failed: %s: %w", string(out), err)
		}

	default:
		// Unknown or script tarball: check if any executable scripts exist, copy to usr/bin
		entries, err := os.ReadDir(workDir)
		if err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					fi, _ := e.Info()
					if fi != nil && (fi.Mode()&0111 != 0) {
						srcP := filepath.Join(workDir, e.Name())
						dstP := filepath.Join(usrBin, e.Name())
						copyFile(srcP, dstP)
					}
				}
			}
		}
	}

	return nil
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
