package main

import (
	"context"
	"io"
	"net/http"
	"time"
)

// stallTimeout is how long a download may go without a response, or without
// another byte of one, before it's given up on. It bounds silence, not the
// whole transfer, so a large model on a slow line still finishes.
var stallTimeout = 30 * time.Second

// httpGet is http.Get that gives up on a server that stops answering.
func httpGet(url string) (*http.Response, error) {
	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(stallTimeout, cancel)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		timer.Stop()
		cancel()
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		timer.Stop()
		cancel()
		if ctx.Err() != nil {
			err = stalled(url)
		}
		return nil, err
	}
	resp.Body = &stallReader{body: resp.Body, url: url, ctx: ctx, timer: timer, cancel: cancel}
	return resp, nil
}

type stallError string

func (e stallError) Error() string { return string(e) }

func stalled(url string) error {
	return stallError("'" + url + "' stopped responding for " + stallTimeout.String())
}

// stallReader restarts the timer on every read that returns.
type stallReader struct {
	body   io.ReadCloser
	url    string
	ctx    context.Context //nolint:containedctx // lives as long as the body
	timer  *time.Timer
	cancel context.CancelFunc
}

func (r *stallReader) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	if err != nil && err != io.EOF && r.ctx.Err() != nil {
		return n, stalled(r.url)
	}
	r.timer.Reset(stallTimeout)
	return n, err
}

func (r *stallReader) Close() error {
	r.timer.Stop()
	r.cancel()
	return r.body.Close()
}
