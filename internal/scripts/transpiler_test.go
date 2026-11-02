package scripts

import (
	"strings"
	"testing"

	"github.com/rfdetoni/mutate/internal/domain"
)

func TestTranspileScriptService(t *testing.T) {
	raw := `#!/bin/sh
service nginx start
chkconfig nginx on
ldconfig
useradd -r -s /bin/false nginx
`
	opts := TranspileOptions{
		TargetFormat:    domain.FormatDEB,
		RemoveLdconfig:  true,
		UseSystemd:      true,
		AdaptUseraddDeb: true,
	}

	res, err := TranspileScript(raw, opts)
	if err != nil {
		t.Fatalf("TranspileScript error: %v", err)
	}

	if !strings.Contains(res, "systemctl start nginx") {
		t.Errorf("expected systemctl start nginx, got:\n%s", res)
	}
	if !strings.Contains(res, "systemctl enable nginx") {
		t.Errorf("expected systemctl enable nginx, got:\n%s", res)
	}
	if strings.Contains(res, "ldconfig") {
		t.Errorf("expected ldconfig to be removed, got:\n%s", res)
	}
	if !strings.Contains(res, "--system") {
		t.Errorf("expected useradd --system, got:\n%s", res)
	}
}

func TestTranspilePackageScripts(t *testing.T) {
	ir := domain.NewPackageIR("", domain.FormatRPM)
	ir.Scripts.PostInstall = "service myapp restart"

	err := TranspilePackageScripts(ir, domain.FormatDEB)
	if err != nil {
		t.Fatalf("transpile error: %v", err)
	}

	if !strings.Contains(ir.Scripts.PostInstall, "systemctl restart myapp") {
		t.Errorf("expected systemctl restart myapp, got %s", ir.Scripts.PostInstall)
	}
}
