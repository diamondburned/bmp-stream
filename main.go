package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gtkcord4/internal/bmp"
	"github.com/edsrzf/mmap-go"
	"github.com/pkg/errors"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	go func() {
		for range time.Tick(time.Second) {
			runtime.GC()
		}
	}()

	app := gtk.NewApplication("com.github.diamondburned.gotk4.giotest", 0)
	app.ConnectActivate(func() { activate(ctx, app) })
	os.Exit(app.Run(os.Args))
}

const FPS = 60

func activate(ctx context.Context, app *gtk.Application) {
	var wg sync.WaitGroup

	// We hope that /run/user/1000 is a tmpfs so the files that we read/write to
	// are actually in memory.
	tmpdir, err := os.MkdirTemp(fmt.Sprintf("/run/user/%d", os.Getuid()), "giotest-")
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	app.ConnectShutdown(func() {
		cancel()
		wg.Wait()

		log.Println("cleaning up", tmpdir)
		if err := os.RemoveAll(tmpdir); err != nil {
			log.Println("warning: failed to remove temp dir:", err)
		}
	})

	wg.Add(1)
	go func() {
		defer wg.Done()

		<-ctx.Done()
		app.Quit()
	}()

	errCh := make(chan error, 1)

	wg.Add(1)
	go func() {
		defer wg.Done()

		// Atomic writing is where the magic happens!
		// Also, "-pix_fmt rgba" actually gives us BGRA.
		err := sh(ctx, fmt.Sprintf(`
			ffmpeg -y \
				-hide_banner -loglevel error -threads 1 \
				-f v4l2 -framerate 60 -i /dev/video0 \
				-c:v bmp -pix_fmt rgba -update 1 -atomic_writing 1 %s/screen.bmp
		`, tmpdir))
		// err := sh(ctx, fmt.Sprintf(`
		// 	ffmpeg -y \
		// 		-hide_banner -loglevel error \
		// 		-stream_loop -1 -re -i ~/Videos/LLOGE.mp4 \
		// 		-c:v bmp -update 1 -atomic_writing 1 %s/screen.bmp
		// `, tmpdir))
		if err != nil {
			select {
			case errCh <- err:
			default:
			}
		}
	}()

	bmpr := newBMPReader(filepath.Join(tmpdir, "screen.bmp"), FPS)

	wg.Add(1)
	go func() {
		defer wg.Done()

		err := bmpr.start(ctx)
		if err != nil {
			select {
			case errCh <- err:
			default:
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		select {
		case <-ctx.Done():
			return
		case err := <-errCh:
			log.Println("error occurred:", err)
			cancel()
		}
	}()

	screen := gtk.NewPicture()
	screen.SetKeepAspectRatio(true)
	screen.AddTickCallback(func(_ gtk.Widgetter, clock gdk.FrameClocker) bool {
		bmpr.acquire(func(txt *gdk.MemoryTexture) { screen.SetPaintable(txt) })
		return true
	})

	glib.TimeoutAdd(1000/FPS, func() bool {
		screen.QueueDraw()
		return ctx.Err() == nil
	})

	win := gtk.NewApplicationWindow(app)
	win.SetDefaultSize(800, 600)
	win.SetChild(screen)
	win.Show()
}

type bmpReader struct {
	path string
	freq time.Duration
	dec  *bmp.BGRADecoder

	bmp  *bmp.NBGRA
	txtv atomic.Value // *gdk.MemoryTexture
}

func newBMPReader(path string, fps int) *bmpReader {
	return &bmpReader{
		path: path,
		freq: time.Second / time.Duration(fps),
		dec:  bmp.NewBGRADecoder(),
	}
}

func (r *bmpReader) start(ctx context.Context) error {
	clock := time.NewTicker(r.freq)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-clock.C:
			// ok
		}

		if err := r.update(ctx); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
}

func (r *bmpReader) update(ctx context.Context) error {
	f, err := os.Open(r.path)
	if err != nil {
		return errors.Wrap(err, "failed to open bmp snapshot")
	}
	defer f.Close()

	buf, err := mmap.Map(f, mmap.RDONLY, 0)
	if err != nil {
		return errors.Wrap(err, "failed to mmap bmp snapshot")
	}
	defer buf.Unmap()

	// TODO: figure out double buffering to avoid locking for too long.
	r.bmp, err = r.dec.Decode(buf, r.bmp)
	if err != nil {
		return errors.Wrap(err, "failed to decode bmp snapshot")
	}

	// This unfortunately makes a newly-allocated copy of the image every call.
	// It's probably the slowest part of this code and why you should write your
	// own Paintable.
	newTexture := gdk.NewMemoryTexture(
		r.bmp.Rect.Dx(),
		r.bmp.Rect.Dy(),
		// I'm not sure if this is the format that GTK uses. They might be
		// swizzling this on their own in the code which adds cost.
		gdk.MemoryB8G8R8A8Premultiplied,
		glib.NewBytesWithGo(r.bmp.Pix),
		uint(r.bmp.Stride),
	)

	r.txtv.Store(newTexture)
	return nil
}

func (r *bmpReader) acquire(f func(*gdk.MemoryTexture)) {
	txt, _ := r.txtv.Swap((*gdk.MemoryTexture)(nil)).(*gdk.MemoryTexture)
	if txt != nil {
		f(txt)
	}
}

func sh(ctx context.Context, shcmd string) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", shcmd)
	cmd.Stdout = nil
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
