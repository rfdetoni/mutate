package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/explorer"
)

var (
	canvas       = color.NRGBA{R: 243, G: 244, B: 246, A: 255}
	white        = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	mutedSurface = color.NRGBA{R: 248, G: 249, B: 250, A: 255}
	line         = color.NRGBA{R: 225, G: 228, B: 232, A: 255}
	ink          = color.NRGBA{R: 32, G: 35, B: 42, A: 255}
	softInk      = color.NRGBA{R: 82, G: 89, B: 101, A: 255}
	mutedInk     = color.NRGBA{R: 116, G: 123, B: 134, A: 255}
	violet       = color.NRGBA{R: 103, G: 84, B: 174, A: 255}
	violetLight  = color.NRGBA{R: 241, G: 238, B: 249, A: 255}
	positive     = color.NRGBA{R: 38, G: 123, B: 85, A: 255}
	negative     = color.NRGBA{R: 178, G: 59, B: 69, A: 255}
)

var sections = []string{"Converter", "Biblioteca", "Repositórios", "Inspecionar", "Instalar", "Histórico", "Configurações"}

type packageInfo struct {
	FileName string `json:"file_name"`
	Size     int64  `json:"size"`
	Format   string `json:"format"`
	Metadata struct {
		Name         string   `json:"name"`
		Version      string   `json:"version"`
		Architecture string   `json:"architecture"`
		Description  string   `json:"description"`
		Dependencies []string `json:"dependencies"`
	} `json:"metadata"`
}
type jobInfo struct {
	Operation         string   `json:"operation"`
	State             string   `json:"state"`
	Phase             string   `json:"phase"`
	Message           string   `json:"message"`
	OutputID          string   `json:"output_id"`
	OutputName        string   `json:"output_name"`
	ELFs              int      `json:"elf_count"`
	ExternalLibraries []string `json:"external_libraries"`
}
type historyInfo struct {
	Name       string    `json:"name"`
	Operation  string    `json:"operation"`
	Target     string    `json:"target"`
	State      string    `json:"state"`
	OutputID   string    `json:"output_id"`
	OutputName string    `json:"output_name"`
	At         time.Time `json:"at"`
}
type appState struct {
	Selected     *packageInfo  `json:"selected"`
	Job          *jobInfo      `json:"job"`
	History      []historyInfo `json:"history"`
	Busy         bool          `json:"busy"`
	Host         string        `json:"host"`
	NativeTarget string        `json:"native_target"`
}
type catalogApp struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Native      bool   `json:"native"`
	Installable bool   `json:"installable"`
}
type repository struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Endpoint string `json:"endpoint"`
	Kind     string `json:"kind"`
	Enabled  bool   `json:"enabled"`
	Builtin  bool   `json:"builtin"`
	Status   string `json:"status"`
	Error    string `json:"error"`
}

type response struct {
	apply   func(*screen)
	err     error
	label   string
	polling bool
}
type screen struct {
	window                                                *app.Window
	chooser                                               *explorer.Explorer
	api                                                   *client
	ctx                                                   context.Context
	cancel                                                context.CancelFunc
	updates                                               chan response
	th                                                    *material.Theme
	view                                                  int
	bodyList                                              widget.List
	asideList                                             widget.List
	nav                                                   [7]widget.Clickable
	pick, filePathOpen, search, refresh, addRepository    widget.Clickable
	inspect, convert, install, download, confirm, decline widget.Clickable
	format                                                widget.Enum
	resolve, extract, allowSource                         widget.Bool
	path, searchQuery, repoName, repoURL, dependencyMap   widget.Editor
	repoButtons                                           map[string]*widget.Clickable
	catalogButtons                                        map[string]*widget.Clickable
	historyButtons                                        map[string]*widget.Clickable
	state                                                 appState
	apps                                                  []catalogApp
	sources                                               []repository
	warnings                                              []string
	waiting                                               bool
	message                                               string
	problem                                               string
	confirmation                                          string
	confirmID                                             string
}

func runWindow(api *client) error {
	w := new(app.Window)
	w.Option(app.Title("M.U.T.A.T.E."), app.Size(unit.Dp(1180), unit.Dp(780)), app.MinSize(unit.Dp(840), unit.Dp(600)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	u := &screen{window: w, chooser: explorer.NewExplorer(w), api: api, ctx: ctx, cancel: cancel,
		updates: make(chan response, 32), th: material.NewTheme(), repoButtons: make(map[string]*widget.Clickable),
		catalogButtons: make(map[string]*widget.Clickable), historyButtons: make(map[string]*widget.Clickable),
		resolve: widget.Bool{Value: true}, format: widget.Enum{Value: "deb"},
	}
	u.th.Palette.Fg = ink
	u.th.Palette.Bg = white
	u.th.Palette.ContrastBg = violet
	u.th.Palette.ContrastFg = white
	u.path.SingleLine = true
	u.searchQuery.SingleLine = true
	u.repoName.SingleLine = true
	u.repoURL.SingleLine = true
	u.dependencyMap.SingleLine = false
	u.dependencyMap.SetText("{}")
	u.bodyList.List.Axis = layout.Vertical
	u.asideList.List.Axis = layout.Vertical
	u.refreshState()
	u.refreshRepos()
	go u.poll()
	var ops op.Ops
	for {
		e := w.Event()
		u.chooser.ListenEvents(e)
		switch event := e.(type) {
		case app.DestroyEvent:
			return event.Err
		case app.FrameEvent:
			u.receive()
			gtx := app.NewContext(&ops, event)
			u.layout(gtx)
			event.Frame(gtx.Ops)
		}
	}
}

func (u *screen) deliver(r response) {
	select {
	case u.updates <- r:
	case <-u.ctx.Done():
	}
	u.window.Invalidate()
}
func (u *screen) receive() {
	for {
		select {
		case r := <-u.updates:
			if !r.polling {
				u.waiting = false
				if r.err != nil {
					u.problem = r.err.Error()
					u.message = ""
				} else if r.label != "" {
					u.problem = ""
					u.message = r.label
				}
			}
			if r.apply != nil {
				r.apply(u)
			}
		default:
			return
		}
	}
}
func (u *screen) runTask(label string, fn func() (func(*screen), error)) {
	if u.waiting {
		return
	}
	u.waiting = true
	u.problem = ""
	u.message = label
	go func() { apply, err := fn(); u.deliver(response{apply: apply, err: err, label: label}) }()
}
func (u *screen) poll() {
	ticker := time.NewTicker(950 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			u.refreshState()
		case <-u.ctx.Done():
			return
		}
	}
}
func (u *screen) refreshState() {
	go func() {
		var s appState
		if err := u.api.get(u.ctx, "state", &s); err == nil {
			u.deliver(response{polling: true, apply: func(u *screen) { u.state = s }})
		}
	}()
}
func (u *screen) refreshRepos() {
	u.runTask("Atualizando fontes", func() (func(*screen), error) {
		var data struct {
			Sources []repository `json:"sources"`
		}
		if err := u.api.get(u.ctx, "repositories", &data); err != nil {
			return nil, err
		}
		return func(u *screen) { u.sources = data.Sources }, nil
	})
}
func (u *screen) doAction(action string) {
	if u.state.Selected == nil || u.state.Busy || u.waiting {
		return
	}
	target := u.format.Value
	if action == "install" {
		target = u.state.NativeTarget
	}
	if target == "" {
		u.problem = "Esta distribuição não permite instalação automática"
		return
	}
	payload := map[string]any{"action": action, "target": target, "resolve": u.resolve.Value,
		"extract": u.extract.Value, "allow_source_build": u.allowSource.Value, "dependency_map": u.dependencyMap.Text()}
	u.runTask("Iniciando "+action, func() (func(*screen), error) {
		var out map[string]any
		if err := u.api.send(u.ctx, http.MethodPost, "action", payload, &out); err != nil {
			return nil, err
		}
		return func(u *screen) { u.message = "Operação iniciada"; u.refreshState() }, nil
	})
}
func (u *screen) pickFile() {
	if u.waiting || u.state.Busy {
		return
	}
	u.runTask("Selecionando pacote", func() (func(*screen), error) {
		f, err := u.chooser.ChooseFile()
		if errors.Is(err, explorer.ErrUserDecline) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("seletor nativo indisponível: %w. Informe o caminho no campo abaixo", err)
		}
		name := "pacote.deb"
		if named, ok := f.(interface{ Name() string }); ok && named.Name() != "" {
			name = filepath.Base(named.Name())
		}
		if err := u.api.upload(u.ctx, f, name); err != nil {
			return nil, err
		}
		return func(u *screen) { u.message = "Pacote carregado"; u.refreshState() }, nil
	})
}
func (u *screen) uploadPath() {
	path := strings.TrimSpace(u.path.Text())
	if path == "" {
		u.problem = "Informe o caminho de um pacote"
		return
	}
	u.runTask("Lendo arquivo", func() (func(*screen), error) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		if err := u.api.upload(u.ctx, f, filepath.Base(path)); err != nil {
			return nil, err
		}
		return func(u *screen) { u.refreshState() }, nil
	})
}
func (u *screen) saveOutput(id, name string) {
	if u.waiting || id == "" {
		return
	}
	u.runTask("Salvando pacote", func() (func(*screen), error) {
		w, err := u.chooser.CreateFile(name)
		if errors.Is(err, explorer.ErrUserDecline) {
			return nil, nil
		}
		if err != nil {
			file, copyErr := u.api.downloadToPath(u.ctx, id, name)
			if copyErr != nil {
				return nil, fmt.Errorf("seletor: %v; pasta Downloads: %w", err, copyErr)
			}
			return func(u *screen) { u.message = "Salvo em " + file }, nil
		}
		copyErr := u.api.download(u.ctx, id, w)
		closeErr := w.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		return func(u *screen) { u.message = "Pacote salvo com sucesso" }, nil
	})
}
func (u *screen) catalogSearch() {
	q := strings.TrimSpace(u.searchQuery.Text())
	if len(q) < 2 {
		u.problem = "Digite pelo menos dois caracteres para pesquisar"
		return
	}
	u.runTask("Pesquisando repositórios", func() (func(*screen), error) {
		var result struct {
			Apps     []catalogApp `json:"apps"`
			Warnings []string     `json:"warnings"`
		}
		if err := u.api.get(u.ctx, "catalog?q="+url.QueryEscape(q), &result); err != nil {
			return nil, err
		}
		return func(u *screen) { u.apps = result.Apps; u.warnings = result.Warnings }, nil
	})
}
func (u *screen) repositoryAction(method, path string, payload any) {
	u.runTask("Atualizando repositório", func() (func(*screen), error) {
		if err := u.api.send(u.ctx, method, path, payload, nil); err != nil {
			return nil, err
		}
		var out struct {
			Sources []repository `json:"sources"`
		}
		if err := u.api.get(u.ctx, "repositories", &out); err != nil {
			return nil, err
		}
		return func(u *screen) { u.sources = out.Sources }, nil
	})
}
func (u *screen) confirmAction() {
	kind, id := u.confirmation, u.confirmID
	u.confirmation = ""
	u.confirmID = ""
	switch kind {
	case "install":
		u.doAction("install")
	case "catalog":
		u.runTask("Instalando aplicativo", func() (func(*screen), error) {
			var out map[string]any
			if err := u.api.send(u.ctx, http.MethodPost, "catalog/install", map[string]string{"id": id}, &out); err != nil {
				return nil, err
			}
			return func(u *screen) { u.refreshState() }, nil
		})
	case "remove":
		u.repositoryAction(http.MethodDelete, "repositories/"+url.PathEscape(id), nil)
	}
}

func gap(gtx layout.Context, v unit.Dp) layout.Dimensions {
	return layout.Spacer{Height: v}.Layout(gtx)
}
func strip(gtx layout.Context) layout.Dimensions {
	width := gtx.Constraints.Max.X
	paint.FillShape(gtx.Ops, line, clip.Rect{Max: image.Pt(width, gtx.Dp(1))}.Op())
	return layout.Dimensions{Size: image.Pt(width, gtx.Dp(1))}
}
func surface(gtx layout.Context, bg color.NRGBA, radius int, child layout.Widget) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			size := gtx.Constraints.Min
			paint.FillShape(gtx.Ops, bg, clip.RRect{Rect: image.Rectangle{Max: size}, NE: radius, NW: radius, SE: radius, SW: radius}.Op(gtx.Ops))
			return layout.Dimensions{Size: size}
		}), layout.Stacked(child))
}
func (u *screen) label(gtx layout.Context, s string, size unit.Sp, c color.NRGBA) layout.Dimensions {
	l := material.Label(u.th, size, s)
	l.Color = c
	return l.Layout(gtx)
}
func (u *screen) title(gtx layout.Context, s string) layout.Dimensions {
	return u.label(gtx, s, unit.Sp(24), ink)
}
func (u *screen) button(gtx layout.Context, b *widget.Clickable, name string, bg, colorText color.NRGBA) layout.Dimensions {
	style := material.Button(u.th, b, name)
	style.Background = bg
	style.Color = colorText
	style.TextSize = unit.Sp(13)
	style.CornerRadius = unit.Dp(7)
	style.Inset = layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(14), Right: unit.Dp(14)}
	return style.Layout(gtx)
}
func (u *screen) field(gtx layout.Context, ed *widget.Editor, hint string) layout.Dimensions {
	return widget.Border{Color: line, CornerRadius: unit.Dp(7), Width: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return surface(gtx, white, 7, func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(9)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				style := material.Editor(u.th, ed, hint)
				style.TextSize = unit.Sp(13)
				return style.Layout(gtx)
			})
		})
	})
}
func (u *screen) card(gtx layout.Context, child layout.Widget) layout.Dimensions {
	return widget.Border{Color: line, CornerRadius: unit.Dp(8), Width: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return surface(gtx, white, 8, func(gtx layout.Context) layout.Dimensions { return layout.UniformInset(unit.Dp(20)).Layout(gtx, child) })
	})
}
func (u *screen) note(gtx layout.Context, txt string, accent color.NRGBA) layout.Dimensions {
	return surface(gtx, mutedSurface, 7, func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions { return u.label(gtx, txt, unit.Sp(13), accent) })
	})
}
func (u *screen) layout(gtx layout.Context) layout.Dimensions {
	paint.Fill(gtx.Ops, canvas)
	return surface(gtx, white, 0, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return u.topbar(gtx) }),
			layout.Rigid(strip),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints = layout.Exact(image.Pt(gtx.Dp(182), gtx.Constraints.Max.Y))
						return u.sidebar(gtx)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return surface(gtx, white, 0, func(gtx layout.Context) layout.Dimensions {
							return material.List(u.th, &u.bodyList).Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
								return layout.UniformInset(unit.Dp(28)).Layout(gtx, u.mainPanel)
							})
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if u.view > 0 && u.view < 3 || u.view > 4 {
							return layout.Dimensions{}
						}
						gtx.Constraints = layout.Exact(image.Pt(gtx.Dp(292), gtx.Constraints.Max.Y))
						return u.inspector(gtx)
					}),
				)
			}),
		)
	})
}
func (u *screen) topbar(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(15), Bottom: unit.Dp(15), Left: unit.Dp(24), Right: unit.Dp(24)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return surface(gtx, violetLight, 7, func(gtx layout.Context) layout.Dimensions {
					return layout.UniformInset(unit.Dp(7)).Layout(gtx, func(gtx layout.Context) layout.Dimensions { return u.label(gtx, "↻", unit.Sp(23), violet) })
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return u.label(gtx, "M.U.T.A.T.E.", unit.Sp(16), ink) })
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: gtx.Constraints.Min} }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return u.label(gtx, "LINUX  ·  "+strings.ToUpper(u.state.Host), unit.Sp(11), mutedInk)
			}),
		)
	})
}
func (u *screen) sidebar(gtx layout.Context) layout.Dimensions {
	return surface(gtx, mutedSurface, 0, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(22), Bottom: unit.Dp(14), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			children := []layout.FlexChild{
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(10), Bottom: unit.Dp(13)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return u.label(gtx, "FERRAMENTAS", unit.Sp(10), mutedInk) })
				}),
			}
			for i, name := range sections {
				i, name := i, name
				if i == 5 {
					children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(18)) }))
				}
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if u.nav[i].Clicked(gtx) {
						u.view = i
						u.problem = ""
						u.message = ""
						u.bodyList.Position = layout.Position{}
						if i == 2 {
							u.refreshRepos()
						}
					}
					bg, fg := mutedSurface, softInk
					if u.view == i {
						bg = violetLight
						fg = violet
					}
					return u.button(gtx, &u.nav[i], name, bg, fg)
				}))
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(5)) }))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	})
}
func (u *screen) header(gtx layout.Context, title, description string) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.label(gtx, "ÁREA DE TRABALHO", unit.Sp(10), violet)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(8)) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return u.title(gtx, title) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(7)) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return u.label(gtx, description, unit.Sp(13), softInk) }),
	)
}
func (u *screen) mainPanel(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if u.confirmation != "" {
				return u.confirmPanel(gtx)
			}
			switch u.view {
			case 0, 3, 4:
				return u.packagePanel(gtx)
			case 1:
				return u.libraryPanel(gtx)
			case 2:
				return u.repositoryPanel(gtx)
			case 5:
				return u.historyPanel(gtx)
			default:
				return u.settingsPanel(gtx)
			}
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if u.waiting || u.state.Busy || u.message != "" || u.problem != "" {
				return gap(gtx, unit.Dp(16))
			}
			return layout.Dimensions{}
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if u.problem != "" {
				return u.note(gtx, "Erro: "+u.problem, negative)
			}
			if u.waiting {
				return u.note(gtx, u.message+"…", violet)
			}
			if u.message != "" {
				return u.note(gtx, u.message, positive)
			}
			return layout.Dimensions{}
		}),
	)
}
func (u *screen) packagePanel(gtx layout.Context) layout.Dimensions {
	titles := []string{"Converta pacotes Linux", "", "", "Inspecione pacotes", "Instale no Linux"}
	intro := []string{"Selecione um pacote, confira os detalhes e escolha o formato de saída.", "", "", "Analise metadados, executáveis e bibliotecas do pacote.", "Converta para o formato do host antes de instalar com autorização."}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return u.header(gtx, titles[u.view], intro[u.view]) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(24)) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.card(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.label(gtx, "SELEÇÃO DE ARQUIVO", unit.Sp(10), violet)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(12)) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						name := "Nenhum arquivo selecionado"
						if u.state.Selected != nil {
							name = u.state.Selected.FileName
						}
						return u.label(gtx, name, unit.Sp(16), ink)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(7)) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.label(gtx, "DEB, RPM, Arch, Alpine APK, AppImage e arquivos fonte", unit.Sp(12), softInk)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(19)) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if u.pick.Clicked(gtx) {
							u.pickFile()
						}
						return u.button(gtx, &u.pick, "Selecionar pacote…", violet, white)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(16)) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.label(gtx, "OU INFORME O CAMINHO DO ARQUIVO", unit.Sp(10), mutedInk)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(6)) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.field(gtx, &u.path, "/home/usuario/Downloads/pacote.deb")
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(10)) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if u.filePathOpen.Clicked(gtx) {
							u.uploadPath()
						}
						return u.button(gtx, &u.filePathOpen, "Abrir caminho", violetLight, violet)
					}),
				)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(20)) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return u.activityPanel(gtx) }),
	)
}
func (u *screen) activityPanel(gtx layout.Context) layout.Dimensions {
	return u.card(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return u.label(gtx, "ATIVIDADE", unit.Sp(10), mutedInk) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(10)) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if u.state.Job == nil {
					return u.label(gtx, "Pronto para analisar um pacote", unit.Sp(15), ink)
				}
				j := u.state.Job
				if j.State == "running" {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return material.Loader(u.th).Layout(gtx) }),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return u.label(gtx, j.Phase, unit.Sp(14), violet) })
						}),
					)
				}
				if j.State == "error" {
					return u.label(gtx, j.Message, unit.Sp(13), negative)
				}
				return u.label(gtx, j.Message, unit.Sp(13), positive)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if u.state.Job == nil || u.state.Job.OutputID == "" {
					return layout.Dimensions{}
				}
				return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					if u.download.Clicked(gtx) {
						u.saveOutput(u.state.Job.OutputID, u.state.Job.OutputName)
					}
					return u.button(gtx, &u.download, "Salvar pacote convertido…", violetLight, violet)
				})
			}),
		)
	})
}
func (u *screen) inspector(gtx layout.Context) layout.Dimensions {
	return surface(gtx, mutedSurface, 0, func(gtx layout.Context) layout.Dimensions {
		return material.List(u.th, &u.asideList).Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
			return layout.UniformInset(unit.Dp(20)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.label(gtx, "DETALHES DO PACOTE", unit.Sp(12), ink)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(12)) }),
					layout.Rigid(strip),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(12)) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if u.state.Selected == nil {
							return u.label(gtx, "Escolha um arquivo para visualizar nome, versão, arquitetura e dependências.", unit.Sp(13), softInk)
						}
						p := u.state.Selected
						items := [][2]string{{"Nome", p.Metadata.Name}, {"Versão", p.Metadata.Version}, {"Arquitetura", p.Metadata.Architecture}, {"Formato", p.Format}, {"Tamanho", fmt.Sprintf("%.1f MiB", float64(p.Size)/1048576)}, {"Dependências", fmt.Sprintf("%d encontradas", len(p.Metadata.Dependencies))}}
						children := make([]layout.FlexChild, 0, len(items)*2)
						for _, pair := range items {
							pair := pair
							children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return u.label(gtx, pair[0], unit.Sp(11), mutedInk) }))
							children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Bottom: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return u.label(gtx, pair[1], unit.Sp(13), ink) })
							}))
						}
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(10)) }),
					layout.Rigid(strip),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(12)) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return u.label(gtx, "FORMATO DE SAÍDA", unit.Sp(11), mutedInk)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return material.RadioButton(u.th, &u.format, "deb", "Debian .deb").Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return material.RadioButton(u.th, &u.format, "rpm", "RPM .rpm").Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return material.RadioButton(u.th, &u.format, "arch", "Arch .pkg.tar.zst").Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return material.RadioButton(u.th, &u.format, "apk", "Alpine .apk").Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(14)) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if u.state.Selected == nil || u.state.Busy || u.waiting {
							return u.label(gtx, "Selecione um pacote para habilitar as ações.", unit.Sp(12), mutedInk)
						}
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								if u.inspect.Clicked(gtx) {
									u.doAction("inspect")
								}
								return u.button(gtx, &u.inspect, "Inspecionar", violetLight, violet)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(8)) }),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								if u.convert.Clicked(gtx) {
									u.doAction("convert")
								}
								return u.button(gtx, &u.convert, "Converter", violet, white)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(8)) }),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								if u.install.Clicked(gtx) {
									u.confirmation = "install"
									u.confirmID = ""
								}
								return u.button(gtx, &u.install, "Instalar no sistema", positive, white)
							}),
						)
					}),
				)
			})
		})
	})
}
func (u *screen) libraryPanel(gtx layout.Context) layout.Dimensions {
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.header(gtx, "Biblioteca de aplicativos", "Pesquise repositórios nativos e catálogos de descoberta.")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(22)) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return u.field(gtx, &u.searchQuery, "Nome do aplicativo") }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(10)) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if u.search.Clicked(gtx) {
				u.catalogSearch()
			}
			return u.button(gtx, &u.search, "Pesquisar", violet, white)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(18)) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.label(gtx, fmt.Sprintf("%d resultados", len(u.apps)), unit.Sp(12), softInk)
		}),
	}
	for _, w := range u.warnings {
		warning := w
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return u.note(gtx, warning, mutedInk) }))
	}
	for _, a := range u.apps {
		item := a
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return u.card(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return u.label(gtx, item.Name+"  ·  "+item.Version, unit.Sp(16), ink)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(6)) }),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return u.label(gtx, item.Description, unit.Sp(12), softInk)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(8)) }),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return u.label(gtx, "Origem: "+item.Source, unit.Sp(11), mutedInk)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if !item.Native || !item.Installable {
								return layout.Dimensions{}
							}
							btn := u.cachedButton(u.catalogButtons, item.ID)
							if btn.Clicked(gtx) {
								u.confirmation = "catalog"
								u.confirmID = item.ID
							}
							return layout.Inset{Top: unit.Dp(9)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return u.button(gtx, btn, "Instalar aplicativo", positive, white)
							})
						}),
					)
				})
			})
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}
func (u *screen) cachedButton(m map[string]*widget.Clickable, key string) *widget.Clickable {
	if button := m[key]; button != nil {
		return button
	}
	button := new(widget.Clickable)
	m[key] = button
	return button
}
func (u *screen) repositoryPanel(gtx layout.Context) layout.Dimensions {
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.header(gtx, "Repositórios", "Fontes nativas e catálogos remotos gerenciados localmente.")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(18)) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if u.refresh.Clicked(gtx) {
				u.repositoryAction(http.MethodPost, "repositories/refresh", map[string]any{})
			}
			return u.button(gtx, &u.refresh, "Atualizar fontes", violetLight, violet)
		}),
	}
	for _, source := range u.sources {
		s := source
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return u.card(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return u.label(gtx, s.Name, unit.Sp(15), ink) }),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(5)) }),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							state := "Desativado"
							if s.Enabled {
								state = "Ativado · " + s.Status
							}
							if s.Error != "" {
								state += " · " + s.Error
							}
							return u.label(gtx, state, unit.Sp(12), softInk)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(10)) }),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							key := s.ID + "-toggle"
							btn := u.cachedButton(u.repoButtons, key)
							if btn.Clicked(gtx) {
								u.repositoryAction(http.MethodPost, "repositories/"+url.PathEscape(s.ID)+"/enabled", map[string]bool{"enabled": !s.Enabled})
							}
							text := "Ativar"
							if s.Enabled {
								text = "Desativar"
							}
							return u.button(gtx, btn, text, violetLight, violet)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if s.Builtin {
								return layout.Dimensions{}
							}
							key := s.ID + "-remove"
							btn := u.cachedButton(u.repoButtons, key)
							if btn.Clicked(gtx) {
								u.confirmation = "remove"
								u.confirmID = s.ID
							}
							return layout.Inset{Top: unit.Dp(7)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return u.button(gtx, btn, "Remover", white, negative) })
						}),
					)
				})
			})
		}))
	}
	children = append(children,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(20)) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.label(gtx, "ADICIONAR CATÁLOGO HTTPS", unit.Sp(11), violet)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(7)) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return u.field(gtx, &u.repoName, "Nome do catálogo") }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(8)) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.field(gtx, &u.repoURL, "https://exemplo.org/search?q={query}")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(10)) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if u.addRepository.Clicked(gtx) {
				u.repositoryAction(http.MethodPost, "repositories", map[string]string{"name": u.repoName.Text(), "url": u.repoURL.Text()})
			}
			return u.button(gtx, &u.addRepository, "Adicionar fonte", violet, white)
		}),
	)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}
func (u *screen) historyPanel(gtx layout.Context) layout.Dimensions {
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.header(gtx, "Histórico", "As últimas operações desta sessão, sem armazenamento persistente.")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(22)) }),
	}
	if len(u.state.History) == 0 {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.note(gtx, "Nenhuma operação concluída nesta sessão.", softInk)
		}))
	}
	for _, entry := range u.state.History {
		e := entry
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return u.card(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return u.label(gtx, e.Name, unit.Sp(15), ink) }),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return u.label(gtx, e.Operation+" · "+e.State+" · "+e.At.Format("02/01 15:04"), unit.Sp(12), softInk)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if e.OutputID == "" {
								return layout.Dimensions{}
							}
							btn := u.cachedButton(u.historyButtons, e.OutputID)
							if btn.Clicked(gtx) {
								u.saveOutput(e.OutputID, e.OutputName)
							}
							return layout.Inset{Top: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return u.button(gtx, btn, "Salvar resultado…", violetLight, violet)
							})
						}),
					)
				})
			})
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}
func (u *screen) settingsPanel(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.header(gtx, "Configurações", "Estas opções são aplicadas às operações desta sessão.")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(22)) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.CheckBox(u.th, &u.resolve, "Resolver bibliotecas do host").Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.CheckBox(u.th, &u.extract, "Extrair AppImage para análise").Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.CheckBox(u.th, &u.allowSource, "Permitir construção de fonte em sandbox").Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(17)) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.label(gtx, "MAPA JSON DE DEPENDÊNCIAS", unit.Sp(11), violet)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(8)) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return u.field(gtx, &u.dependencyMap, `{"so:libfoo.so.1":"libfoo1"}`)
		}),
	)
}
func (u *screen) confirmPanel(gtx layout.Context) layout.Dimensions {
	title, message := "Confirmar operação", ""
	switch u.confirmation {
	case "install":
		title = "Instalar no sistema?"
		message = "O Mutate converterá o pacote e solicitará autorização do administrador. Scripts do pacote podem executar com privilégios elevados."
	case "catalog":
		title = "Instalar aplicativo do repositório?"
		message = "O gerenciador nativo instalará o aplicativo e suas dependências. A autorização do sistema será solicitada."
	case "remove":
		title = "Remover repositório?"
		message = "A fonte personalizada deixará de aparecer nas pesquisas."
	}
	return u.card(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return u.title(gtx, title) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(12)) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return u.label(gtx, message, unit.Sp(13), softInk) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return gap(gtx, unit.Dp(20)) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if u.decline.Clicked(gtx) {
							u.confirmation = ""
							u.confirmID = ""
						}
						return u.button(gtx, &u.decline, "Cancelar", violetLight, violet)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return layout.Spacer{Width: unit.Dp(10)}.Layout(gtx) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if u.confirm.Clicked(gtx) {
							u.confirmAction()
						}
						return u.button(gtx, &u.confirm, "Confirmar", positive, white)
					}),
				)
			}),
		)
	})
}
