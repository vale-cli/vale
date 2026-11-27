package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func shortStall(t *testing.T) {
	t.Helper()
	old := stallTimeout
	stallTimeout = 100 * time.Millisecond
	t.Cleanup(func() { stallTimeout = old })
}

// A server that never answers, and one that stops partway through the
// body, both end the download instead of hanging it.
func TestDownloadGivesUpOnStalledServer(t *testing.T) {
	shortStall(t)

	for name, partial := range map[string]bool{"no response": false, "partial body": true} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if partial {
					_, _ = w.Write([]byte("PK"))
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
			}))
			defer srv.Close()

			done := make(chan error, 1)
			go func() {
				_, err := fetchPinned(srv.URL+"/Style.zip", t.TempDir(), "")
				done <- err
			}()

			select {
			case err := <-done:
				if err == nil || !strings.Contains(err.Error(), "stopped responding") {
					t.Fatalf("expected a stall error, got %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the download hung on a stalled server")
			}
		})
	}
}

// A server slower than the timeout overall, but never silent for that long,
// is not a stall.
func TestDownloadAllowsSlowServer(t *testing.T) {
	shortStall(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for range 6 {
			_, _ = w.Write([]byte("x"))
			w.(http.Flusher).Flush()
			time.Sleep(40 * time.Millisecond)
		}
	}))
	defer srv.Close()

	b, err := fetchJSON(srv.URL)
	if err != nil || string(b) != "xxxxxx" {
		t.Fatalf("expected the whole body, got %q (%v)", b, err)
	}
}

func TestSyncReportsUnreachableLibrary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	old := library
	library = srv.URL
	t.Cleanup(func() { library = old })

	_, err := syncPkg("Microsoft", t.TempDir(), 0)
	if err == nil || !strings.Contains(err.Error(), "could not look up 'Microsoft' in the package library") {
		t.Fatalf("expected the library to be named as the failure, got %v", err)
	}
	if strings.Contains(err.Error(), "not a valid URL") {
		t.Fatalf("the name was blamed for a network failure: %v", err)
	}
}
