package nlp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/jdkato/prose/v3/tag"
)

type SegmentResult struct {
	Sents []string
}

type TagResult struct {
	Tokens []tag.Token
}

// post sends the request and returns its body, but only for a successful
// (2xx) response.
//
// Without this check, a remote endpoint returning e.g. `500 {"sents":[]}` --
// an error status with a technically-valid-but-degenerate JSON body -- was
// decoded exactly as if it had succeeded: doSegment or pos would hand back a
// zero-value result and a nil error, and a caller reading that as "no
// sentences" or "no tokens" rather than "the request failed" would silently
// carry on with wrong data instead of surfacing the real problem.
func post(url string) ([]byte, error) {
	var body []byte

	resp, err := http.Post(url, "application/x-www-form-urlencoded", nil) //nolint:gosec,noctx
	if err != nil {
		return body, err
	}
	defer resp.Body.Close()

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return body, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("nlp: request to %s failed: %s", url, resp.Status)
	}

	return body, nil
}

func doSegment(text, lang, apiURL string) (SegmentResult, error) {
	var result SegmentResult

	data := url.Values{"lang": {lang}, "text": {text}}
	path := apiURL + "/segment?" + data.Encode()

	body, err := post(path)
	if err != nil {
		return result, err
	}

	err = json.Unmarshal(body, &result)
	if err != nil {
		return result, err
	}

	return result, nil
}

func pos(text, lang, apiURL string) (TagResult, error) {
	var result TagResult

	data := url.Values{"lang": {lang}, "text": {text}}
	path := apiURL + "/tag?" + data.Encode()

	body, err := post(path)
	if err != nil {
		return result, err
	}

	err = json.Unmarshal(body, &result)
	if err != nil {
		return result, err
	}

	return result, nil
}
