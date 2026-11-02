package scripts

import (
	"github.com/rfdetoni/mutate/internal/domain"
	"strings"
	"testing"
)

func TestExtractArchHooks(t *testing.T) {
	src := "helper() { echo common; }\npost_install() { helper; echo done; }\npre_remove() { echo remove; }"
	out, err := ExtractArchInstall(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.PostInstall, "echo done") || !strings.Contains(out.PostInstall, "helper()") || !strings.Contains(out.PreRemove, "echo remove") {
		t.Fatalf("bad hooks: %+v", out)
	}
	if strings.Contains(out.PostInstall, "post_install()") {
		t.Fatal("hook declaration must be stripped")
	}
}
func TestRejectArchUpgradeHooks(t *testing.T) {
	if _, err := ExtractArchInstall("post_upgrade() { echo changed; }"); err == nil {
		t.Fatal("must not drop upgrade hooks")
	}
}
func TestPortableLifecycleScripts(t *testing.T) {
	ir := domain.NewPackageIR("", domain.FormatRPM)
	ir.Scripts.PostInstall = "#!/bin/sh\necho installed\n"
	if err := TranspilePackageScripts(ir, domain.FormatDEB); err != nil {
		t.Fatal(err)
	}
	ir2 := domain.NewPackageIR("", domain.FormatRPM)
	ir2.Scripts.PreInstall = "echo \"$1\"\n"
	if err := TranspilePackageScripts(ir2, domain.FormatDEB); err == nil {
		t.Fatal("positional argument must fail")
	}
}
