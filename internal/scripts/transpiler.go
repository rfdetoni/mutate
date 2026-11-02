package scripts

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/rfdetoni/mutate/internal/domain"
	"mvdan.cc/sh/v3/syntax"
)

// TranspileOptions configures script AST translation rules.
type TranspileOptions struct {
	TargetFormat     domain.Format
	RemoveLdconfig   bool
	UseSystemd       bool
	AdaptUseraddDeb  bool
}

// TranspileScript parses a shell script into an AST, applies transformations, and prints formatted shell script.
func TranspileScript(rawScript string, opts TranspileOptions) (string, error) {
	if strings.TrimSpace(rawScript) == "" {
		return "", nil
	}

	parser := syntax.NewParser()
	file, err := parser.Parse(strings.NewReader(rawScript), "")
	if err != nil {
		// If script cannot be parsed as valid POSIX shell AST, return original script safely
		return "",fmt.Errorf("script is not valid shell and cannot safely be translated: %w",err)
 }

	syntax.Walk(file, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}

		cmd := call.Args[0].Lit()

		// Rule 1: Replace legacy `service <unit> <action>` with `systemctl <action> <unit>`
		if opts.UseSystemd && cmd == "service" && len(call.Args) >= 3 {
			unit := call.Args[1].Lit()
			action := call.Args[2].Lit()
			call.Args = []*syntax.Word{
				makeWord("systemctl"),
				makeWord(action),
				makeWord(unit),
			}
			return false
		}

		// Rule 2: Replace `chkconfig <unit> on/off` with `systemctl enable/disable <unit>`
		if opts.UseSystemd && cmd == "chkconfig" && len(call.Args) >= 3 {
			unit := call.Args[1].Lit()
			action := call.Args[2].Lit()
			sysAction := "enable"
			if action == "off" || action == "stop" {
				sysAction = "disable"
			}
			call.Args = []*syntax.Word{
				makeWord("systemctl"),
				makeWord(sysAction),
				makeWord(unit),
			}
			return false
		}

		// Rule 3: Adapt useradd/adduser flags between RedHat and Debian
		if opts.AdaptUseraddDeb && cmd == "useradd" {
			for i, arg := range call.Args {
				if arg.Lit() == "-r" {
					call.Args[i] = makeWord("--system")
				}
			}
		}

		// Rule 4: Remove redundant ldconfig calls (replace with ':' no-op)
		if opts.RemoveLdconfig && cmd == "ldconfig" {
			call.Args = []*syntax.Word{makeWord(":")}
			return false
		}

		return true
	})

	var buf bytes.Buffer
	printer := syntax.NewPrinter()
	if err := printer.Print(&buf, file); err != nil {
		return "", fmt.Errorf("print transpiled script AST: %w", err)
	}

	return buf.String(), nil
}

// TranspilePackageScripts applies AST transpilation to all scripts in PackageIR.
func TranspilePackageScripts(ir *domain.PackageIR, targetFormat domain.Format) error {
 if targetFormat==domain.FormatUnknown{return fmt.Errorf("unsupported target script format")}
 if ir.Format==domain.FormatArch && targetFormat!=domain.FormatArch && (ir.Scripts.PreInstall!=""||ir.Scripts.PostInstall!=""||ir.Scripts.PreRemove!=""||ir.Scripts.PostRemove!="") {
  return fmt.Errorf("Arch .INSTALL hooks require lifecycle-aware translation; refusing unsafe conversion")
 }
	opts := TranspileOptions{
		TargetFormat:    targetFormat,
		RemoveLdconfig:  false,
  UseSystemd:      targetFormat != domain.FormatAlpineAPK,
		AdaptUseraddDeb: (targetFormat == domain.FormatDEB),
	}

	var err error
	if ir.Scripts.PreInstall != "" {
		if ir.Scripts.PreInstall, err = TranspileScript(ir.Scripts.PreInstall, opts); err != nil {
			return err
		}
	}
	if ir.Scripts.PostInstall != "" {
		if ir.Scripts.PostInstall, err = TranspileScript(ir.Scripts.PostInstall, opts); err != nil {
			return err
		}
	}
	if ir.Scripts.PreRemove != "" {
		if ir.Scripts.PreRemove, err = TranspileScript(ir.Scripts.PreRemove, opts); err != nil {
			return err
		}
	}
	if ir.Scripts.PostRemove != "" {
		if ir.Scripts.PostRemove, err = TranspileScript(ir.Scripts.PostRemove, opts); err != nil {
			return err
		}
	}

	return nil
}

func makeWord(lit string) *syntax.Word {
	return &syntax.Word{
		Parts: []syntax.WordPart{
			&syntax.Lit{Value: lit},
		},
	}
}
