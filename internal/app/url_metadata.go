package app

import (
	"context"
	"sync"
)

type urlMetadataFetchOwner interface {
	Fetch(context.Context, string) (urlMetadata, error)
}

type urlMetadataIdleCloser interface {
	CloseIdleConnections()
}

type urlMetadataFlight struct {
	cancel  context.CancelFunc
	done    chan struct{}
	err     error
	result  urlMetadata
	waiters int
}

type urlMetadataService struct {
	cancel  context.CancelFunc
	closed  bool
	fetcher urlMetadataFetchOwner
	flights map[string]*urlMetadataFlight
	mu      sync.Mutex
	root    context.Context
	wait    sync.WaitGroup
}

func newURLMetadataService(fetcher urlMetadataFetchOwner) *urlMetadataService {
	root, cancel := context.WithCancel(context.Background())
	return &urlMetadataService{
		cancel: cancel, fetcher: fetcher, flights: map[string]*urlMetadataFlight{}, root: root,
	}
}

func (service *urlMetadataService) Fetch(ctx context.Context, rawURL string) (urlMetadata, error) {
	return service.fetch(ctx, rawURL, false)
}

// FetchVideoSize reads a page as Fetch does and also its video's shape, from
// the still its oEmbed answer names: two more reads, made only for a caller
// that draws a video by it (a streamed video's frame), not for every link.
func (service *urlMetadataService) FetchVideoSize(ctx context.Context, rawURL string) (urlMetadata, error) {
	return service.fetch(ctx, rawURL, true)
}

func (service *urlMetadataService) fetch(ctx context.Context, rawURL string, videoSize bool) (urlMetadata, error) {
	normalized, err := normalizeURLMetadataURL(rawURL)
	if err != nil {
		return urlMetadata{}, err
	}
	// A read that wants the video's shape is its own flight: one that doesn't
	// must not wait on, or hand back, the longer read.
	key := normalized
	if videoSize {
		key += "\x00video"
	}
	service.mu.Lock()
	if service.closed {
		service.mu.Unlock()
		return urlMetadata{}, context.Canceled
	}
	flight := service.flights[key]
	if flight == nil {
		flightContext, cancel := context.WithCancel(service.root)
		if videoSize {
			flightContext = context.WithValue(flightContext, urlMetadataVideoSizeKey{}, true)
		}
		flight = &urlMetadataFlight{cancel: cancel, done: make(chan struct{})}
		service.flights[key] = flight
		service.wait.Add(1)
		go service.runFlight(flightContext, key, normalized, flight)
	}
	flight.waiters++
	service.mu.Unlock()

	select {
	case <-flight.done:
		service.releaseWaiter(key, flight, false)
		return flight.result, flight.err
	case <-ctx.Done():
		service.releaseWaiter(key, flight, true)
		return urlMetadata{}, ctx.Err()
	}
}

// urlMetadataVideoSizeKey marks a read whose caller wants the video's shape too.
type urlMetadataVideoSizeKey struct{}

func urlMetadataWantsVideoSize(ctx context.Context) bool {
	wanted, _ := ctx.Value(urlMetadataVideoSizeKey{}).(bool)
	return wanted
}

func (service *urlMetadataService) runFlight(
	ctx context.Context, key, normalized string, flight *urlMetadataFlight,
) {
	defer service.wait.Done()
	defer flight.cancel()
	result, err := service.fetcher.Fetch(ctx, normalized)
	service.mu.Lock()
	flight.result, flight.err = result, err
	if service.flights[key] == flight {
		delete(service.flights, key)
	}
	close(flight.done)
	service.mu.Unlock()
}

func (service *urlMetadataService) releaseWaiter(
	key string, flight *urlMetadataFlight, cancelIfLast bool,
) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if flight.waiters > 0 {
		flight.waiters--
	}
	if cancelIfLast && flight.waiters == 0 && service.flights[key] == flight {
		delete(service.flights, key)
		flight.cancel()
	}
}

func (service *urlMetadataService) Shutdown(ctx context.Context) error {
	service.mu.Lock()
	if !service.closed {
		service.closed = true
		service.cancel()
		for _, flight := range service.flights {
			flight.cancel()
		}
	}
	service.mu.Unlock()
	done := make(chan struct{})
	go func() {
		service.wait.Wait()
		close(done)
	}()
	select {
	case <-done:
		if closer, ok := service.fetcher.(urlMetadataIdleCloser); ok {
			closer.CloseIdleConnections()
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
