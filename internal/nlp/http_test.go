package nlp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jdkato/prose/v3/tag"
)

// A remote endpoint returning a non-2xx status with an otherwise
// well-formed JSON body -- e.g. `500 {"sents":[]}` -- must not be read as a
// successful, empty result. Without a status check, json.Unmarshal has
// nothing to fail on: the caller gets a nil error and a zero-value result,
// indistinguishable from "the endpoint really has nothing to report" --
// silently wrong instead of a surfaced failure.
func TestPostRejectsNonTwoXXStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"sents":[]}`))
	}))
	defer server.Close()

	body, err := post(server.URL)
	if err == nil {
		t.Fatalf("post returned a nil error for a %d response with a valid JSON body",
			http.StatusInternalServerError)
	}
	if body != nil {
		t.Errorf("body = %q, want nil alongside the error", body)
	}
}

// post is the shared transport both doSegment (/segment) and pos (/tag) call
// through, so a status check there has to cover both -- this confirms it
// does for /segment.
func TestDoSegmentReturnsErrorOnNonTwoXXStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"Sents":[]}`))
	}))
	defer server.Close()

	if _, err := doSegment("some text", "id", server.URL); err == nil {
		t.Fatalf("doSegment returned a nil error for a %d /segment response",
			http.StatusInternalServerError)
	}
}

// Same check for /tag, the tagging counterpart to /segment.
func TestPosReturnsErrorOnNonTwoXXStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"Tokens":[]}`))
	}))
	defer server.Close()

	if _, err := pos("some text", "id", server.URL); err == nil {
		t.Fatalf("pos returned a nil error for a %d /tag response",
			http.StatusInternalServerError)
	}
}

// TextToTokens used to panic when a configured remote endpoint's /tag
// request failed -- a network error, a non-2xx status, a malformed response
// -- which crashed the whole vale process. Its only production caller,
// TextToContext (internal/core/util.go, reached from the `tag` CLI command),
// already had somewhere sensible to route an error: this confirms
// TextToTokens itself now returns one instead of panicking, matching the
// same fix already made to Info.Compute (see
// TestComputeReturnsErrorOnSegmentEndpointFailure in provider_test.go).
func TestTextToTokensReturnsErrorOnTagEndpointFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"Tokens":[]}`))
	}))
	defer server.Close()

	var toks []tag.Token
	var err error
	func() {
		defer func() {
			if p := recover(); p != nil {
				t.Fatalf("TextToTokens panicked instead of returning an error: %v", p)
			}
		}()
		toks, err = TextToTokens("some text", &Info{Lang: "id", Endpoint: server.URL})
	}()

	if err == nil {
		t.Fatalf("TextToTokens returned a nil error for a failed /tag request, want a non-nil error")
	}
	if toks != nil {
		t.Errorf("tokens = %v, want nil alongside the error", toks)
	}
}

// Control: an ordinary 2xx response must still be read normally.
func TestPostSucceedsOnTwoXXStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"sents":["ok"]}`))
	}))
	defer server.Close()

	body, err := post(server.URL)
	if err != nil {
		t.Fatalf("post returned an error for a 200 response: %v", err)
	}
	if !strings.Contains(string(body), "ok") {
		t.Errorf("body = %q, want it to contain the response payload", body)
	}
}
