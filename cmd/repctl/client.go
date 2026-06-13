package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a thin authenticated wrapper around the RePanel REST API.
type Client struct {
	URL   string
	Token string
	HTTP  *http.Client
}

func newClient(c Config) *Client {
	tr := &http.Transport{}
	if c.Insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in for self-signed panels
	}
	return &Client{
		URL:   strings.TrimRight(c.URL, "/"),
		Token: c.Token,
		HTTP:  &http.Client{Transport: tr, Timeout: 30 * time.Second},
	}
}

// apiError is the panel's standard error envelope.
type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("request failed (HTTP %d)", e.Status)
}

// do performs a request and returns the raw response body. Non-2xx responses
// are turned into an *apiError carrying the panel's error message.
func (cl *Client) do(method, path string, body []byte) ([]byte, error) {
	if cl.URL == "" {
		return nil, fmt.Errorf("no panel URL configured — run `repctl login` or set --url/REPANEL_URL")
	}
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, cl.URL+path, r)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cl.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cl.Token)
	}
	resp, err := cl.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		msg := ""
		var env struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &env) == nil {
			msg = env.Error
		}
		return nil, &apiError{Status: resp.StatusCode, Message: msg}
	}
	return data, nil
}

// getJSON performs a GET and decodes the JSON body into out.
func (cl *Client) getJSON(path string, out any) error {
	data, err := cl.do("GET", path, nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

// postJSON marshals body, POSTs it, and decodes the response into out (out may
// be nil to ignore the response).
func (cl *Client) postJSON(path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	data, err := cl.do("POST", path, b)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}
