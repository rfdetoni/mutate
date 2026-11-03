package unpacker

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/avast/apkparser"
	"github.com/rfdetoni/mutate/internal/domain"
)

type AndroidManifestXML struct {
	XMLName     xml.Name `xml:"manifest"`
	Package     string   `xml:"package,attr"`
	VersionCode string   `xml:"versionCode,attr"`
	VersionName string   `xml:"versionName,attr"`
	Application struct {
		Label    string `xml:"label,attr"`
		Icon     string `xml:"icon,attr"`
		Activity []struct {
			Name         string `xml:"name,attr"`
			IntentFilter []struct {
				Action []struct {
					Name string `xml:"name,attr"`
				} `xml:"action"`
				Category []struct {
					Name string `xml:"name,attr"`
				} `xml:"category"`
			} `xml:"intent-filter"`
		} `xml:"activity"`
	} `xml:"application"`
}

type AndroidUnpacker struct{}

func NewAndroidUnpacker() *AndroidUnpacker {
	return &AndroidUnpacker{}
}

func (u *AndroidUnpacker) Unpack(srcPath string, stagingDir string) (*domain.PackageIR, error) {
	// Parse binary AndroidManifest.xml into standard XML string
	var xmlBuf bytes.Buffer
	enc := xml.NewEncoder(&xmlBuf)
	_, _, err := apkparser.ParseApk(srcPath, enc)
	if err != nil {
		return nil, fmt.Errorf("parse AndroidManifest.xml: %w", err)
	}

	var manifest AndroidManifestXML
	if err := xml.Unmarshal(xmlBuf.Bytes(), &manifest); err != nil {
		return nil, fmt.Errorf("unmarshal AndroidManifest: %w", err)
	}

	pkgName := manifest.Package
	if pkgName != "" && !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$`).MatchString(pkgName) {
		return nil, fmt.Errorf("invalid Android package name %q", pkgName)
	}
	if pkgName == "" {
		pkgName = "android-app"
	}

	appName := sanitizeAppName(pkgName)
	appLabel := manifest.Application.Label
	appLabel = strings.NewReplacer("\n", " ", "\r", " ").Replace(appLabel)
	if appLabel == "" {
		appLabel = appName
	}

	ir := domain.NewPackageIR(stagingDir, domain.FormatAndroidAPK)
	ir.Metadata = domain.Metadata{
		Name:         appName,
		Version:      defaultString(manifest.VersionName, "1.0.0"),
		Release:      defaultString(manifest.VersionCode, "1"),
		Architecture: "all",
		Description:  fmt.Sprintf("Android application %s (%s) bridged for Waydroid", appLabel, pkgName),
		License:      "Proprietary",
		Dependencies: []string{"waydroid"},
	}

	// Preserve the complete APK verbatim. nFPM can read directly from the
	// original source without an unnecessary intermediate filesystem copy.
	st, err := os.Lstat(srcPath)
	if err != nil {
		return nil, fmt.Errorf("stat APK: %w", err)
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("Android APK must be a regular file")
	}
	absoluteSrc, err := filepath.Abs(srcPath)
	if err != nil {
		return nil, err
	}
	apkTargetDest := fmt.Sprintf("/usr/share/android-apps/%s.apk", appName)
	ir.Files[apkTargetDest] = domain.FileEntry{
		SourcePath: absoluteSrc, DestinationPath: apkTargetDest, Mode: 0644,
		ModTime: st.ModTime(),
	}

	// 2. Extract icon if available
	iconDest := fmt.Sprintf("/usr/share/icons/hicolor/scalable/apps/%s.png", appName)
	iconDisk := filepath.Join(stagingDir, strings.TrimPrefix(iconDest, "/"))
	if err := extractApkIcon(srcPath, iconDisk); err == nil {
		ir.Files[iconDest] = domain.FileEntry{
			SourcePath:      iconDisk,
			DestinationPath: iconDest,
			Mode:            0644,
		}
	}

	// 3. Generate launcher script in /usr/bin/<appName>
	binDest := fmt.Sprintf("/usr/bin/%s", appName)
	binDisk := filepath.Join(stagingDir, strings.TrimPrefix(binDest, "/"))
	if err := os.MkdirAll(filepath.Dir(binDisk), 0755); err != nil {
		return nil, err
	}

	launcherScript := generateWaydroidLauncher(pkgName, apkTargetDest)
	if err := os.WriteFile(binDisk, []byte(launcherScript), 0755); err != nil {
		return nil, fmt.Errorf("write waydroid launcher: %w", err)
	}
	ir.Files[binDest] = domain.FileEntry{
		SourcePath:      binDisk,
		DestinationPath: binDest,
		Mode:            0755,
	}

	// 4. Generate .desktop file in /usr/share/applications/<appName>.desktop
	desktopDest := fmt.Sprintf("/usr/share/applications/%s.desktop", appName)
	desktopDisk := filepath.Join(stagingDir, strings.TrimPrefix(desktopDest, "/"))
	if err := os.MkdirAll(filepath.Dir(desktopDisk), 0755); err != nil {
		return nil, err
	}

	desktopContent := fmt.Sprintf("[Desktop Entry]\nType=Application\nName=%s\nExec=%s\nIcon=%s\nCategories=Utility;Application;\nTerminal=false\n", appLabel, binDest, appName)
	if err := os.WriteFile(desktopDisk, []byte(desktopContent), 0644); err != nil {
		return nil, fmt.Errorf("write desktop file: %w", err)
	}
	ir.Files[desktopDest] = domain.FileEntry{
		SourcePath:      desktopDisk,
		DestinationPath: desktopDest,
		Mode:            0644,
	}

	return ir, nil
}

func sanitizeAppName(pkg string) string {
	parts := strings.Split(pkg, ".")
	last := parts[len(parts)-1]
	return strings.ToLower(strings.ReplaceAll(last, "_", "-"))
}

func extractApkIcon(apkPath, outIconPath string) error {
	zr, err := zip.OpenReader(apkPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	var iconFile *zip.File
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, ".png") && (strings.Contains(f.Name, "ic_launcher") || strings.Contains(f.Name, "icon")) {
			iconFile = f
			break
		}
	}

	if iconFile == nil {
		return os.ErrNotExist
	}

	rc, err := iconFile.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	if err := os.MkdirAll(filepath.Dir(outIconPath), 0755); err != nil {
		return err
	}

	out, err := os.Create(outIconPath)
	if err != nil {
		return err
	}
	defer out.Close()

	n, err := io.Copy(out, io.LimitReader(rc, 16<<20+1))
	if err != nil {
		return err
	}
	if n > 16<<20 {
		return fmt.Errorf("APK icon exceeds 16 MiB")
	}
	return nil
}

func generateWaydroidLauncher(packageName, apkPath string) string {
	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	sb.WriteString("# Generated by M.U.T.A.T.E. Android-to-Linux Waydroid Bridge\n")
	sb.WriteString("set -e\n\n")
	sb.WriteString(fmt.Sprintf("PKG=\"%s\"\n", packageName))
	sb.WriteString(fmt.Sprintf("APK=\"%s\"\n\n", apkPath))
	sb.WriteString(`if ! command -v waydroid >/dev/null 2>&1; then
    echo "Error: waydroid is required to run Android applications." >&2
    exit 1
fi

if ! waydroid app list 2>/dev/null | grep -q "$PKG"; then
    echo "Installing $PKG into Waydroid..."
    waydroid app install "$APK"
fi

exec waydroid app launch "$PKG" "$@"
`)
	return sb.String()
}
