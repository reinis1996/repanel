package system

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Cloudflare API v4 client (DNS records only), authenticated with a scoped API
// token. Used to import/export a RePanel zone's records to/from Cloudflare.

const cfAPIBase = "https://api.cloudflare.com/client/v4"

// CFRecord is the subset of a Cloudflare DNS record the panel syncs.
type CFRecord struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type"`
	Name     string `json:"name"`    // FQDN
	Content  string `json:"content"` // record value
	TTL      int    `json:"ttl"`     // 1 = automatic
	Priority *int   `json:"priority,omitempty"`
	Proxied  bool   `json:"proxied"`
}

type cfEnvelope struct {
	Success    bool            `json:"success"`
	Errors     []cfError       `json:"errors"`
	Result     json.RawMessage `json:"result"`
	ResultInfo struct {
		Page       int `json:"page"`
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// CFClient talks to the Cloudflare API with one API token.
type CFClient struct{ Token string }

func (c CFClient) do(method, path string, body any) (cfEnvelope, error) {
	var buf io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, cfAPIBase+path, buf)
	if err != nil {
		return cfEnvelope{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return cfEnvelope{}, fmt.Errorf("cloudflare request failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	var env cfEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return cfEnvelope{}, fmt.Errorf("cloudflare returned an unexpected response (%s)", resp.Status)
	}
	if !env.Success {
		return env, fmt.Errorf("cloudflare: %s", cfErrMsg(env, resp.Status))
	}
	return env, nil
}

func cfErrMsg(env cfEnvelope, status string) string {
	if len(env.Errors) > 0 {
		return env.Errors[0].Message
	}
	return status
}

// CFVerify checks the token can access the zone (used when saving the binding).
func (c CFClient) CFVerify(zoneID string) error {
	if c.Token == "" || zoneID == "" {
		return fmt.Errorf("a Cloudflare API token and zone id are required")
	}
	_, err := c.do(http.MethodGet, "/zones/"+url.PathEscape(zoneID), nil)
	return err
}

// CFListRecords returns every DNS record in the zone, following pagination.
func (c CFClient) CFListRecords(zoneID string) ([]CFRecord, error) {
	var out []CFRecord
	for page := 1; page <= 100; page++ {
		env, err := c.do(http.MethodGet,
			fmt.Sprintf("/zones/%s/dns_records?per_page=100&page=%d", url.PathEscape(zoneID), page), nil)
		if err != nil {
			return nil, err
		}
		var recs []CFRecord
		json.Unmarshal(env.Result, &recs)
		out = append(out, recs...)
		if env.ResultInfo.TotalPages == 0 || page >= env.ResultInfo.TotalPages {
			break
		}
	}
	return out, nil
}

func (c CFClient) CFCreateRecord(zoneID string, r CFRecord) error {
	_, err := c.do(http.MethodPost, "/zones/"+url.PathEscape(zoneID)+"/dns_records", r)
	return err
}

func (c CFClient) CFUpdateRecord(zoneID, recID string, r CFRecord) error {
	_, err := c.do(http.MethodPut, "/zones/"+url.PathEscape(zoneID)+"/dns_records/"+url.PathEscape(recID), r)
	return err
}

func (c CFClient) CFDeleteRecord(zoneID, recID string) error {
	_, err := c.do(http.MethodDelete, "/zones/"+url.PathEscape(zoneID)+"/dns_records/"+url.PathEscape(recID), nil)
	return err
}
