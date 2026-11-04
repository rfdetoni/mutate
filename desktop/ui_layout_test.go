package main

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/unit"
)

func TestSurfacePaintAreaFollowsParentWidth(t *testing.T) {
	gtx := layout.Context{
		Ops: new(op.Ops),
		Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{
			Min: image.Pt(900, 0),
			Max: image.Pt(900, 600),
		},
	}
	var childMin image.Point
	dims := surface(gtx, color.NRGBA{R: 30, G: 34, B: 42, A: 255}, 10,
		func(child layout.Context) layout.Dimensions {
			childMin = child.Constraints.Min
			return layout.Dimensions{Size: image.Pt(200, 90)}
		})
	if dims.Size.X != 900 || dims.Size.Y != 90 {
		t.Fatalf("background area %v, want 900x90", dims.Size)
	}
	if childMin.X != 0 {
		t.Fatalf("child controls forced to full width: min=%v", childMin)
	}
}

func TestWorkspaceCardUsesEntireAvailableWidth(t *testing.T) {
	gtx := layout.Context{
		Ops: new(op.Ops),
		Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{
			Max: image.Pt(860, 600),
		},
	}
	u := new(screen)
	dims := u.workspaceCard(gtx, func(child layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: image.Pt(250, 100)}
	})
	if dims.Size.X != gtx.Constraints.Max.X {
		t.Fatalf("card width=%d, want %d", dims.Size.X, gtx.Constraints.Max.X)
	}
}

// Package workflows must not register any scroll widget. Only long lists do.
func TestStaticPackageWorkspaceRouting(t *testing.T) {
	for _, tc := range []struct {
		name string
		view int
		confirmation string
		want bool
	}{
		{"converter", 0, "", true},
		{"biblioteca", 1, "", false},
		{"repositorios", 2, "", false},
		{"inspecionar", 3, "", true},
		{"instalar", 4, "", true},
		{"historico", 5, "", false},
		{"configuracoes", 6, "", false},
		{"confirmacao", 1, "install", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := staticPackageWorkspace(tc.view, tc.confirmation); got != tc.want {
				t.Fatalf("staticPackageWorkspace(%d, %q) = %v, want %v", tc.view, tc.confirmation, got, tc.want)
			}
		})
	}
}

// Both compact and wide library cards must fill the available center column.
// A blank package version must not generate an extra version separator.
func TestLibraryResultCardUsesAvailableWidth(t *testing.T) {
	for _, width := range []int{440, 900} {
		gtx := layout.Context{
			Ops: new(op.Ops),
			Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Constraints{Max: image.Pt(width, 600)},
		}
		u := &screen{th: material.NewTheme(), catalogButtons: map[string]*widget.Clickable{}}
		dims := u.libraryResultCard(gtx, catalogApp{
			ID: "test-package", Name: "Firefox", Source: "APT",
			Description: "Navegador de internet",
		})
		if dims.Size.X != width {
			t.Errorf("width %d: result card is %dpx; want %dpx", width, dims.Size.X, width)
		}
	}
}
