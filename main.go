package main

import (
	"context"
	"fmt"
	"image"
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

	"github.com/diamondburned/gotk4/pkg/cairo"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gtkcord4/internal/bmp"
	"github.com/diamondburned/gtkcord4/internal/syncg"
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
		// 		-hide_banner -loglevel error -threads 1 \
		// 		-stream_loop -1 -re -i ~/Videos/LLOGE.mp4 \
		// 		-c:v bmp -pix_fmt rgba -update 1 -atomic_writing 1 %s/screen.bmp
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

	var set bool
	screen := gtk.NewDrawingArea()
	screen.SetDrawFunc(func(screen *gtk.DrawingArea, cr *cairo.Context, width, height int) {
		if !set {
			bmpr.acquire(func(s *cairo.Surface, rect image.Rectangle) {
				cr.SetSourceSurface(s, 0, 0)
			})
		} else {
			bmpr.refresh()
		}
		cr.Paint()
	})

	go func() {
		for {
			if bmpr.isDirty() {
				screen.QueueDraw()
			}
		}
	}()
	// glib.TimeoutAdd(1000/FPS, func() bool {
	// 	screen.QueueDraw()
	// 	return true
	// 	// return ctx.Err() == nil
	// })

	win := gtk.NewApplicationWindow(app)
	win.SetDefaultSize(800, 600)
	win.SetChild(screen)
	win.Show()
}

type cairoSurface struct {
	*cairo.Surface
	Pix []uint8
}

type bmpReader struct {
	path string
	freq time.Duration
	dec  *bmp.BGRADecoder

	// gtk thread
	surface syncg.AtomicValue[*cairoSurface]

	// our (reader) thread
	mut   sync.Mutex
	bmp   *bmp.NBGRA
	dirty uint32
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

var updateLatency = [10]time.Duration{}
var updatePrintCount int

func (r *bmpReader) update(ctx context.Context) error {
	start := time.Now()
	defer func() {
		end := time.Now()
		delta := end.Sub(start)
		updateLatency[updatePrintCount%len(updateLatency)] = delta
		updatePrintCount++

		if updatePrintCount%len(updateLatency) == 0 {
			var sum time.Duration
			for _, d := range updateLatency {
				sum += d
			}
			log.Println("update latency:", sum/time.Duration(len(updateLatency)))
		}
	}()

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
	r.mut.Lock()
	r.bmp, err = r.dec.Decode(buf, r.bmp)
	r.mut.Unlock()
	if err != nil {
		return errors.Wrap(err, "failed to decode bmp snapshot")
	}

	if r.surface.IsZero() {
		// Thankfully in our case, alpha does not matter. When we say FormatARGB in
		// Cairo, it actually means BGRA. See gotk4/pkg/cairo/surface_image.go.
		surface := cairo.CreateImageSurface(cairo.FormatARGB32, r.bmp.Rect.Dx(), r.bmp.Rect.Dy())
		surfacePix := surface.Data()

		r.surface.Store(&cairoSurface{
			Surface: surface,
			Pix:     surfacePix,
		})
	}

	atomic.StoreUint32(&r.dirty, 1)
	return nil
}

func (r *bmpReader) isDirty() bool {
	return atomic.SwapUint32(&r.dirty, 0) == 1
}

var blitLatency = [10]time.Duration{}
var blitPrintCount int

func (r *bmpReader) refresh() bool {
	start := time.Now()
	defer func() {
		end := time.Now()
		delta := end.Sub(start)
		blitLatency[updatePrintCount%len(updateLatency)] = delta
		blitPrintCount++

		if blitPrintCount%len(blitLatency) == 0 {
			var sum time.Duration
			for _, d := range blitLatency {
				sum += d
			}
			log.Println("blit latency:", sum/time.Duration(len(blitLatency)))
		}
	}()

	surface, ok := r.surface.Load()
	if !ok {
		// no surface yet
		return false
	}

	surface.Flush()

	r.mut.Lock()
	copy(surface.Data(), r.bmp.Pix)
	r.mut.Unlock()

	surface.MarkDirty()

	return true
}

func (r *bmpReader) acquire(f func(*cairo.Surface, image.Rectangle)) {
	if !r.refresh() {
		return
	}

	surface, _ := r.surface.Load()
	rect := r.bmp.Rect // we don't write this anymore after the first run

	f(surface.Surface, rect)
}

func sh(ctx context.Context, shcmd string) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", shcmd)
	cmd.Stdout = nil
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
