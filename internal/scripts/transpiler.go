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
	TargetFormat    domain.Format
	RemoveLdconfig  bool
	UseSystemd      bool
	AdaptUseraddDeb bool
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
		return "", fmt.Errorf("script is not valid shell and cannot safely be translated: %w", err)
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
	if targetFormat == domain.FormatUnknown {
		return fmt.Errorf("unsupported target script format")
	}
	if ir.Format != targetFormat {
		for _, raw := range []string{ir.Scripts.PreInstall, ir.Scripts.PostInstall, ir.Scripts.PreRemove, ir.Scripts.PostRemove} {
			if err := validatePortableLifecycle(raw); err != nil {
				return err
			}
		}
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

// ExtractArchInstall separates lifecycle function bodies from an Arch .INSTALL
// file. Arch functions cannot be carried verbatim as Debian/RPM scriptlets.
func ExtractArchInstall(raw string) (domain.Scripts, error) {
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(raw), "")
	if err != nil {
		return domain.Scripts{}, fmt.Errorf("invalid Arch .INSTALL: %w", err)
	}
	hooks := make(map[string]*syntax.FuncDecl)
	var helpers []*syntax.Stmt
	for _, stmt := range file.Stmts {
		fn, ok := stmt.Cmd.(*syntax.FuncDecl)
		if !ok {
			return domain.Scripts{}, fmt.Errorf("unsupported top-level statement in Arch .INSTALL")
		}
		switch fn.Name.Value {
		case "pre_install", "post_install", "pre_remove", "post_remove":
			if _, exists := hooks[fn.Name.Value]; exists {
				return domain.Scripts{}, fmt.Errorf("duplicate Arch hook %s", fn.Name.Value)
			}
			hooks[fn.Name.Value] = fn
		case "pre_upgrade", "post_upgrade":
			return domain.Scripts{}, fmt.Errorf("Arch upgrade hook %s requires upgrade-specific output support", fn.Name.Value)
		default:
			helpers = append(helpers, stmt)
		}
	}
	extract := func(name string) (string, error) {
		fn := hooks[name]
		if fn == nil {
			return "", nil
		}
		body, ok := fn.Body.Cmd.(*syntax.Block)
		if !ok {
			return "", fmt.Errorf("unsupported Arch hook body %s", name)
		}
		all := append(append([]*syntax.Stmt{}, helpers...), body.Stmts...)
		var buf bytes.Buffer
		if err := syntax.NewPrinter().Print(&buf, &syntax.File{Stmts: all}); err != nil {
			return "", err
		}
		return buf.String(), nil
	}
	var result domain.Scripts
	if result.PreInstall, err = extract("pre_install"); err != nil {
		return result, err
	}
	if result.PostInstall, err = extract("post_install"); err != nil {
		return result, err
	}
	if result.PreRemove, err = extract("pre_remove"); err != nil {
		return result, err
	}
	if result.PostRemove, err = extract("post_remove"); err != nil {
		return result, err
	}
	return result, nil
}

func validatePortableLifecycle(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	f, err := syntax.NewParser().Parse(strings.NewReader(raw), "")
	if err != nil {
		return fmt.Errorf("unportable script syntax: %w", err)
	}
	var problem string
	banned := map[string]bool{"dpkg": true, "dpkg-query": true, "apt-get": true, "apt": true, "rpm": true, "dnf": true, "yum": true, "pacman": true, "apk": true, "update-rc.d": true, "invoke-rc.d": true}
	syntax.Walk(f, func(n syntax.Node) bool {
		if x, ok := n.(*syntax.ParamExp); ok {
			if x.Param.Value == "@" || x.Param.Value == "#" || (len(x.Param.Value) == 1 && x.Param.Value[0] >= '0' && x.Param.Value[0] <= '9') {
				problem = "source-specific lifecycle positional parameter"
			}
		}
		if call, ok := n.(*syntax.CallExpr); ok && len(call.Args) > 0 && banned[call.Args[0].Lit()] {
			problem = "distribution-specific package-manager command"
		}
		return true
	})
	if problem != "" {
		return fmt.Errorf("cannot safely translate lifecycle script: %s", problem)
	}
	return nil
}
