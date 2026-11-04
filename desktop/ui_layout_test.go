package main

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
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
