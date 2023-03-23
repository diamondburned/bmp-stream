#include <gdk/gdk.h>
#include <glib-2.0/glib.h>

gboolean gdk_texture_set_render_data(GdkTexture *self, gpointer key,
                                     gpointer data, GDestroyNotify notify);
