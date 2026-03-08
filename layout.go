package main

import "fyne.io/fyne/v2"

type outOfSafe struct {
	c fyne.Canvas
}

func outOfSafeLayout(w fyne.Window) fyne.Layout {
	return outOfSafe{c: w.Canvas()}
}

func (o outOfSafe) Layout(objs []fyne.CanvasObject, _ fyne.Size) {
	inset, _ := o.c.InteractiveArea()
	outset := fyne.NewPos(-inset.X, -inset.Y)
	full := o.c.Size()

	for _, o := range objs {
		o.Move(outset)
		o.Resize(full)
	}
}

func (o outOfSafe) MinSize(objs []fyne.CanvasObject) fyne.Size {
	return objs[0].MinSize()
}
