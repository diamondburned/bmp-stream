package gdkhack

// #cgo pkg-config: gtk4
// #include "./gdktextureprivate.h"
import "C"

import "github.com/diamondburned/gotk4/pkg/gdk/v4"

func TextureSetRenderData(texture *gdk.Texture)
