package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/pterm/pterm"

	"github.com/vale-cli/vale/v3/internal/core"
)

// Response is returned after an action.
type Response struct {
	Msg     string
	Error   string
	Success bool
}

func progressError(context string, err error, p *pterm.ProgressbarPrinter) error {
	_, _ = p.Stop()
	return core.NewE100(context, err)
}

func pluralize(s string, n int) string {
	if n != 1 {
		return s + "s"
	}
	return s
}

func getJSON(data interface{}) string {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err.Error()
	}
	return string(b)
}

func fetchJSON(url string) ([]byte, error) {
	resp, err := httpGet(url)
	if err != nil {
		return []byte{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return []byte{}, fmt.Errorf("could not fetch '%s' (status code '%d')", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func printJSON(t interface{}) error {
	b, err := json.MarshalIndent(t, "", "    ")
	if err != nil {
		fmt.Println("{}")
		return err
	}
	fmt.Println(string(b))
	return nil
}

// Send a JSON response after a local action.
func sendResponse(msg string, err error) error {
	resp := Response{Msg: msg, Success: err == nil}
	if !resp.Success {
		resp.Error = err.Error()
	}
	return printJSON(resp)
}

func toCodeStyle(s string) string {
	return pterm.Fuzzy.Sprint(s)
}
