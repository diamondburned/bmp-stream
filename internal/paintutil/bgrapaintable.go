package paintutil

import (
	"io"
	"unsafe"

	"github.com/diamondburned/gotk4/pkg/core/gbox"
	"github.com/diamondburned/gotk4/pkg/gio/v2"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
)

/*
#cgo pkg-config: gtk4
#cgo CFLAGS: -Wno-deprecated-declarations
#include <gdk/gdk.h>

typedef struct {
	void* buf;
	gint width; // also stride
	gint height;
} GoBGRAPaintablePrivate;

typedef struct {
	GObjectClass parent_class;
} GoBGRAPaintableClass;

typedef struct {
	GObject parent_instance;
	GoBGRAPaintablePrivate* priv;
} GoBGRAPaintable;

G_DEFINE_TYPE_WITH_CODE(
	GoBGRAPaintable, go_bgra_paintable,
	G_TYPE_OBJECt,
	G_IMPLEMENT_INTERFACE(GDK_TYPE_PAINTABLE, go_bgra_paintable_paintable_iface_init)
);

#define GO_BGRA_PAINTABLE(o) (G_TYPE_CHECK_INSTANCE_CAST((o), go_bgra_paintable_get_type(), GoBGRAPaintable))

static void go_bgra_paintable_init(GoBGRAPaintable *stream) {};

extern gssize goInputStreamRead(guintptr id, void* buf, gsize len, GError** errOut);
extern gssize goOutputStreamWrite(guintptr id, const void* buf, gsize len, GError** errOut);
extern gboolean goStreamClose(guintptr id, GError** errOut);

static gssize go_input_stream_read(GInputStream* stream, void* buf, gsize len, GCancellable* cancel, GError** errOut) {
	GoInputStream* gostream = GO_INPUT_STREAM(stream);
	GoInputStreamPrivate *priv = gostream->priv;
	return goInputStreamRead(priv->id, buf, len, errOut);
};

static gboolean go_input_stream_close(GInputStream* stream, GCancellable* cancel, GError** errOut) {
	GoInputStream* gostream = GO_INPUT_STREAM(stream);
	GoInputStreamPrivate *priv = gostream->priv;
	return goStreamClose(priv->id, errOut);
};

static void go_input_stream_class_init(GoInputStreamClass *klass) {
	GInputStreamClass* istream_class = G_INPUT_STREAM_CLASS(klass);
	istream_class->read_fn = go_input_stream_read;
	istream_class->close_fn = go_input_stream_close;
};

GInputStream* go_input_stream_new(guintptr id) {
	GoInputStream* stream = g_object_new(go_input_stream_get_type(), NULL);

	GoInputStreamPrivate *priv = stream->priv;
	priv->id = id;

	return G_INPUT_STREAM(stream);
};

static gssize go_output_stream_write(GOutputStream* stream, const void* buf, gsize len, GCancellable* cancel, GError** errOut) {
	GoOutputStream* gostream = GO_OUTPUT_STREAM(stream);
	GoOutputStreamPrivate *priv = gostream->priv;
	return goOutputStreamWrite(priv->id, buf, len, errOut);
};

static gboolean go_output_stream_close(GOutputStream* stream, GCancellable* cancel, GError** errOut) {
	GoOutputStream* gostream = GO_OUTPUT_STREAM(stream);
	GoOutputStreamPrivate *priv = gostream->priv;
	return goStreamClose(priv->id, errOut);
};

static void go_output_stream_class_init(GoOutputStreamClass *klass) {
	GOutputStreamClass* ostream_class = G_OUTPUT_STREAM_CLASS(klass);
	ostream_class->write_fn = go_output_stream_write;
	ostream_class->close_fn = go_output_stream_close;
};

GOutputStream* go_output_stream_new(guintptr id) {
	GoOutputStream* stream = g_object_new(go_output_stream_get_type(), NULL);

	GoOutputStreamPrivate *priv = stream->priv;
	priv->id = id;

	return G_OUTPUT_STREAM(stream);
};
*/
import "C"

// NewInputStream creates a new InputStream for the given io.Reader. If r
// implements io.Closer, then it is automatically called if needed.
func NewInputStream(r io.Reader) *gio.InputStream {
	id := gbox.Assign(r)
	ob := C.go_input_stream_new(C.guintptr(id))
	return &gio.InputStream{
		Object: coreglib.AssumeOwnership(unsafe.Pointer(ob)),
	}
}

// NewOutputStream creates a new OutputStream for the given io.Reader. If r
// implements io.Closer, then it is automatically called if needed.
func NewOutputStream(w io.Writer) *gio.OutputStream {
	id := gbox.Assign(w)
	ob := C.go_output_stream_new(C.guintptr(id))
	return &gio.OutputStream{
		Object: coreglib.AssumeOwnership(unsafe.Pointer(ob)),
	}
}
