package media

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/ribeirogab/zapgate/internal/clock"
	"github.com/ribeirogab/zapgate/internal/store"
	"github.com/ribeirogab/zapgate/internal/whatsapp"
)

const (
	MaxConcurrentDownloads = 4
	MaxAttempts            = 12
	firstRetryDelay        = time.Minute
	maxRetryDelay          = 6 * time.Hour
	pollInterval           = time.Minute
	attemptTimeout         = 10 * time.Minute
)

var closedChannel = func() chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}()

var (
	ErrUnavailable    = errors.New("WhatsApp cannot deliver this file")
	ErrNotDownloaded  = errors.New("the file is not downloaded yet")
	ErrStillRunning   = errors.New("the download is still running")
	errNoSession      = fmt.Errorf("%w: the account has no WhatsApp session", whatsapp.ErrNotConnected)
	errNoDownloadKeys = fmt.Errorf("%w: the message has no download key", whatsapp.ErrMediaGone)
)

type Clients interface {
	Client(accountID int64) (whatsapp.Client, bool)
}

type DownloaderDeps struct {
	Store   *store.Store
	Files   *Files
	Clients Clients
	Clock   clock.Clock
	Log     *slog.Logger
}

type Downloader struct {
	deps  DownloaderDeps
	life  context.Context
	stop  context.CancelFunc
	slots chan struct{}
	wake  chan struct{}
	wg    sync.WaitGroup

	mu       sync.Mutex
	stopped  bool
	inFlight map[store.MediaRef]chan struct{}
}

func NewDownloader(deps DownloaderDeps) *Downloader {
	life, stop := context.WithCancel(context.Background())
	return &Downloader{
		deps:     deps,
		life:     life,
		stop:     stop,
		slots:    make(chan struct{}, MaxConcurrentDownloads),
		wake:     make(chan struct{}, 1),
		inFlight: map[store.MediaRef]chan struct{}{},
	}
}

func (d *Downloader) Run(ctx context.Context) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		d.startDue()
		select {
		case <-ctx.Done():
			d.refuseNewWork()
			d.wg.Wait()
			return nil
		case <-d.wake:
		case <-ticker.C:
		}
	}
}

func (d *Downloader) refuseNewWork() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopped = true
	d.stop()
}

func (d *Downloader) Wake() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *Downloader) Notice(accountID int64, evt any) {
	switch evt.(type) {
	case *events.Connected:
		d.save(d.deps.Store.RetryFailedMediaNow(d.life, accountID, d.deps.Clock.Now()))
		d.Wake()
	case *events.Message, *events.HistorySync:
		d.Wake()
	}
}

func (d *Downloader) Ensure(ctx context.Context, ref store.MediaRef) (store.MediaFile, error) {
	file, err := d.deps.Store.MediaFile(ctx, ref)
	if err != nil {
		return file, err
	}
	if settled(file) {
		return file, outcome(file)
	}
	select {
	case <-d.begin(file):
	case <-ctx.Done():
		return file, ErrStillRunning
	}
	file, err = d.deps.Store.MediaFile(ctx, ref)
	if err != nil {
		return file, err
	}
	return file, outcome(file)
}

func settled(file store.MediaFile) bool {
	return file.Media.State == store.MediaDownloaded || file.Media.State == store.MediaUnavailable
}

func outcome(file store.MediaFile) error {
	switch file.Media.State {
	case store.MediaDownloaded:
		return nil
	case store.MediaUnavailable:
		return fmt.Errorf("%w: %s", ErrUnavailable, file.Error)
	}
	if file.Error == "" {
		return ErrNotDownloaded
	}
	return fmt.Errorf("%w: the last attempt failed (%s)", ErrNotDownloaded, file.Error)
}

func (d *Downloader) startDue() {
	free := cap(d.slots) - d.busy()
	if free <= 0 {
		return
	}
	due, err := d.deps.Store.DueMedia(d.life, d.deps.Clock.Now(), free+d.busy())
	d.save(err)
	for _, file := range due {
		if free == 0 {
			return
		}
		if !d.running(file.MediaRef) {
			d.begin(file)
			free--
		}
	}
}

func (d *Downloader) busy() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.inFlight)
}

func (d *Downloader) running(ref store.MediaRef) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.inFlight[ref]
	return ok
}

func (d *Downloader) begin(file store.MediaFile) <-chan struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	if done, ok := d.inFlight[file.MediaRef]; ok {
		return done
	}
	if d.stopped {
		return closedChannel
	}
	done := make(chan struct{})
	d.inFlight[file.MediaRef] = done
	d.wg.Go(func() {
		defer d.finish(file.MediaRef, done)
		d.slots <- struct{}{}
		defer func() { <-d.slots }()
		if fresh, ok := d.stillDue(file.MediaRef); ok {
			d.attempt(fresh)
		}
	})
	return done
}

func (d *Downloader) stillDue(ref store.MediaRef) (store.MediaFile, bool) {
	file, err := d.deps.Store.MediaFile(d.life, ref)
	if errors.Is(err, store.ErrNotFound) {
		return file, false
	}
	d.save(err)
	return file, err == nil && !settled(file)
}

func (d *Downloader) finish(ref store.MediaRef, done chan struct{}) {
	d.mu.Lock()
	delete(d.inFlight, ref)
	d.mu.Unlock()
	close(done)
	d.Wake()
}

func (d *Downloader) attempt(file store.MediaFile) {
	ctx, cancel := context.WithTimeout(d.life, attemptTimeout)
	defer cancel()
	path, err := d.download(ctx, file)
	if d.life.Err() != nil {
		return
	}
	if err != nil {
		d.recordFailure(file, err)
		return
	}
	kept, err := d.deps.Store.SetMediaDownloaded(d.life, file.MediaRef, path)
	if err != nil || !kept {
		d.save(err)
		d.save(d.deps.Files.Remove(path))
	}
}

func (d *Downloader) download(ctx context.Context, file store.MediaFile) (string, error) {
	if len(file.Media.MediaKey) == 0 || file.Media.DirectPath == "" {
		return "", errNoDownloadKeys
	}
	client, ok := d.deps.Clients.Client(file.AccountID)
	if !ok {
		return "", errNoSession
	}
	part, err := d.deps.Files.createPartial()
	if err != nil {
		return "", err
	}
	if err := client.DownloadMedia(ctx, requestFor(file), part); err != nil {
		part.discard()
		return "", err
	}
	return part.keep(file.AccountID)
}

func requestFor(file store.MediaFile) whatsapp.MediaDownload {
	m := file.Media
	return whatsapp.MediaDownload{
		DirectPath:    m.DirectPath,
		MediaKey:      m.MediaKey,
		FileSHA256:    m.FileSHA256,
		FileEncSHA256: m.FileEncSHA256,
		Type:          mediaType(file.Kind),
	}
}

func mediaType(kind string) whatsmeow.MediaType {
	switch kind {
	case store.MessageVideo:
		return whatsmeow.MediaVideo
	case store.MessageAudio:
		return whatsmeow.MediaAudio
	case store.MessageDocument:
		return whatsmeow.MediaDocument
	}
	return whatsmeow.MediaImage
}

func (d *Downloader) recordFailure(file store.MediaFile, err error) {
	failure := d.failure(file, err)
	d.deps.Log.Warn("cannot download a media file", "account_id", file.AccountID, "attempts", failure.Attempts, "state", failure.State, "error", err)
	d.save(d.deps.Store.SetMediaFailed(d.life, file.MediaRef, failure))
}

func (d *Downloader) failure(file store.MediaFile, err error) store.MediaFailure {
	now := d.deps.Clock.Now()
	failure := store.MediaFailure{State: store.MediaFailed, Attempts: file.Attempts, Error: err.Error()}
	switch {
	case errors.Is(err, whatsapp.ErrMediaGone):
		failure.State = store.MediaUnavailable
	case errors.Is(err, whatsapp.ErrNotConnected):
		failure.RetryAt = now.Add(maxRetryDelay)
	case file.Attempts+1 >= MaxAttempts:
		failure.Attempts++
		failure.State = store.MediaUnavailable
	default:
		failure.Attempts++
		failure.RetryAt = now.Add(retryDelay(failure.Attempts))
	}
	return failure
}

func retryDelay(attempts int) time.Duration {
	delay := firstRetryDelay
	for range attempts - 1 {
		delay *= 2
		if delay >= maxRetryDelay {
			return maxRetryDelay
		}
	}
	return delay
}

func (d *Downloader) save(err error) {
	if err != nil && d.life.Err() == nil {
		d.deps.Log.Error("cannot record a media download", "error", err)
	}
}
