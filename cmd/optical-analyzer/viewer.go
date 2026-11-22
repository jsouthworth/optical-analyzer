package main

import (
	"image"
	"runtime"

	qt "github.com/mappu/miqt/qt6"
)

// ImageViewer wraps a QGraphicsView to display an *image.RGBA with smooth
// wheel zooming, drag panning, fit-to-window resizing, and pixel hover callbacks.
type ImageViewer struct {
	View        *qt.QGraphicsView
	scene       *qt.QGraphicsScene
	pixmapItem  *qt.QGraphicsPixmapItem
	image       *image.RGBA
	userZoomed  bool
	onHover     func(x, y float64)
	onHoverLeft func()
}

func NewImageViewer(parent *qt.QWidget) *ImageViewer {
	v := &ImageViewer{}

	v.scene = qt.NewQGraphicsScene()
	v.View = qt.NewQGraphicsView2()
	if parent != nil {
		v.View.SetParent(parent)
	}
	v.View.SetScene(v.scene)
	v.View.SetRenderHint(qt.QPainter__Antialiasing)
	v.View.SetRenderHint(qt.QPainter__SmoothPixmapTransform)
	v.View.SetDragMode(qt.QGraphicsView__ScrollHandDrag)
	v.View.SetTransformationAnchor(qt.QGraphicsView__AnchorUnderMouse)
	v.View.SetAlignment(qt.AlignCenter)

	v.pixmapItem = qt.NewQGraphicsPixmapItem()
	v.pixmapItem.SetTransformationMode(qt.SmoothTransformation)
	v.pixmapItem.SetAcceptHoverEvents(true)
	v.scene.AddItem(v.pixmapItem.QGraphicsItem)

	v.pixmapItem.OnHoverMoveEvent(func(super func(event *qt.QGraphicsSceneHoverEvent), event *qt.QGraphicsSceneHoverEvent) {
		if v.onHover != nil && v.image != nil {
			pos := event.Pos()
			x, y := pos.X(), pos.Y()
			b := v.image.Bounds()
			if x >= 0 && y >= 0 && x < float64(b.Dx()) && y < float64(b.Dy()) {
				v.onHover(x, y)
			}
		}
		super(event)
	})

	v.pixmapItem.OnHoverLeaveEvent(func(super func(event *qt.QGraphicsSceneHoverEvent), event *qt.QGraphicsSceneHoverEvent) {
		if v.onHoverLeft != nil {
			v.onHoverLeft()
		}
		super(event)
	})

	v.pixmapItem.OnMouseDoubleClickEvent(func(super func(event *qt.QGraphicsSceneMouseEvent), event *qt.QGraphicsSceneMouseEvent) {
		v.FitToWindow()
		super(event)
	})

	v.View.OnWheelEvent(func(super func(event *qt.QWheelEvent), event *qt.QWheelEvent) {
		delta := event.AngleDelta()
		dy := delta.Y()
		if dy > 0 {
			v.ZoomIn()
		} else if dy < 0 {
			v.ZoomOut()
		}
	})

	v.View.OnResizeEvent(func(super func(event *qt.QResizeEvent), event *qt.QResizeEvent) {
		super(event)
		if !v.userZoomed && v.image != nil {
			v.fitInternal()
		}
	})

	return v
}

func (v *ImageViewer) SetHoverCallbacks(onHover func(x, y float64), onLeave func()) {
	v.onHover = onHover
	v.onHoverLeft = onLeave
}

func (v *ImageViewer) SetImage(img *image.RGBA) {
	if img == nil {
		return
	}
	firstLoad := v.image == nil
	v.image = img

	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 || len(img.Pix) == 0 {
		return
	}

	qimg := qt.NewQImage6(&img.Pix[0], w, h, int64(img.Stride), qt.QImage__Format_RGBA8888)
	pixmap := qt.QPixmap_FromImage(qimg)
	runtime.KeepAlive(img)
	qimg.Delete()

	v.pixmapItem.SetPixmap(pixmap)

	v.scene.SetSceneRect2(0, 0, float64(w), float64(h))
	if firstLoad || !v.userZoomed {
		v.fitInternal()
	}
}

func (v *ImageViewer) Clear() {
	v.image = nil
	v.userZoomed = false
	v.pixmapItem.SetPixmap(qt.NewQPixmap())
}

func (v *ImageViewer) Image() *image.RGBA {
	return v.image
}

func (v *ImageViewer) ZoomIn() {
	v.userZoomed = true
	v.View.Scale(1.2, 1.2)
}

func (v *ImageViewer) ZoomOut() {
	v.userZoomed = true
	v.View.Scale(1.0/1.2, 1.0/1.2)
}

func (v *ImageViewer) ActualSize() {
	v.userZoomed = true
	v.View.ResetTransform()
}

func (v *ImageViewer) FitToWindow() {
	v.userZoomed = false
	v.fitInternal()
}

func (v *ImageViewer) fitInternal() {
	if v.image == nil {
		return
	}
	rect := v.pixmapItem.BoundingRect()
	if rect.Width() > 0 && rect.Height() > 0 {
		v.View.FitInView3(rect, qt.KeepAspectRatio)
	}
}
