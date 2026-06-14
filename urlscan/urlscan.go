// Package urlscan is the library behind the urlscan command line:
// the HTTP client, request shaping, and the typed data models for urlscan.io.
//
// The Client here is the spine every command shares. It sets a real
// User-Agent, paces requests so a busy session stays polite, and retries the
// transient failures (429 and 5xx) that any public site throws under load.
// Build your endpoint calls and JSON decoding on top of it.
package urlscan

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// DefaultUserAgent identifies the client to urlscan.io. A real, honest
// User-Agent is both polite and the thing most likely to keep you unblocked.
const DefaultUserAgent = "urlscan/dev (+https://github.com/tamnd/urlscan-cli)"

// Host is the site this client talks to.
const Host = "urlscan.io"

// BaseURL is the root every request is built from.
const BaseURL = "https://" + Host

// ScanResult is a single entry from a search response.
type ScanResult struct {
	UUID    string `kit:"id" json:"uuid"`
	URL     string `json:"url"`
	Domain  string `json:"domain"`
	Time    string `json:"time"`
	Method  string `json:"method"`
	Title   string `json:"title"`
	IP      string `json:"ip"`
	Country string `json:"country"`
	Server  string `json:"server"`
}

// ScanDetail is the full detail for a single scan result.
type ScanDetail struct {
	UUID     string   `kit:"id" json:"uuid"`
	URL      string   `json:"url"`
	Domain   string   `json:"domain"`
	Title    string   `json:"title"`
	IP       string   `json:"ip"`
	Country  string   `json:"country"`
	City     string   `json:"city"`
	Server   string   `json:"server"`
	ASN      string   `json:"asn"`
	ASNName  string   `json:"asn_name"`
	IPs      []string `json:"ips"`
	Domains  []string `json:"domains"`
	Requests int      `json:"requests"`
}

// Client talks to urlscan.io over HTTP.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	// Rate is the minimum gap between requests. Zero means no pacing.
	Rate    time.Duration
	Retries int

	last time.Time
}

// Config carries optional overrides a host or user passes in.
type Config struct {
	UserAgent string
	Rate      time.Duration
	Retries   int
	Timeout   time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		UserAgent: DefaultUserAgent,
		Rate:      time.Second,
		Retries:   5,
		Timeout:   30 * time.Second,
	}
}

// NewClient returns a Client with sensible defaults: a 30s timeout, a 1s
// minimum gap between requests, and five retries on transient errors.
func NewClient() *Client {
	cfg := DefaultConfig()
	return &Client{
		HTTP:      &http.Client{Timeout: cfg.Timeout},
		UserAgent: cfg.UserAgent,
		Rate:      cfg.Rate,
		Retries:   cfg.Retries,
	}
}

// Search queries the urlscan.io search endpoint and returns matching results.
func (c *Client) Search(ctx context.Context, query string, size int) ([]*ScanResult, error) {
	if size <= 0 {
		size = 10
	}
	u := BaseURL + "/api/v1/search/?q=" + url.QueryEscape(query) + "&size=" + strconv.Itoa(size)
	body, err := c.Get(ctx, u)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Results []struct {
			ID   string `json:"_id"`
			Task struct {
				URL    string `json:"url"`
				Time   string `json:"time"`
				UUID   string `json:"uuid"`
				Domain string `json:"domain"`
				Method string `json:"method"`
			} `json:"task"`
			Page struct {
				URL     string `json:"url"`
				Domain  string `json:"domain"`
				IP      string `json:"ip"`
				Country string `json:"country"`
				City    string `json:"city"`
				Server  string `json:"server"`
				Title   string `json:"title"`
			} `json:"page"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}

	out := make([]*ScanResult, 0, len(resp.Results))
	for _, r := range resp.Results {
		sr := &ScanResult{
			UUID:    r.ID,
			URL:     r.Task.URL,
			Domain:  r.Task.Domain,
			Time:    r.Task.Time,
			Method:  r.Task.Method,
			Title:   r.Page.Title,
			IP:      r.Page.IP,
			Country: r.Page.Country,
			Server:  r.Page.Server,
		}
		if sr.UUID == "" {
			sr.UUID = r.Task.UUID
		}
		out = append(out, sr)
	}
	return out, nil
}

// Result fetches the full detail for a single scan by UUID.
func (c *Client) Result(ctx context.Context, uuid string) (*ScanDetail, error) {
	u := BaseURL + "/api/v1/result/" + uuid + "/"
	body, err := c.Get(ctx, u)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Page struct {
			URL     string `json:"url"`
			Domain  string `json:"domain"`
			Title   string `json:"title"`
			IP      string `json:"ip"`
			Country string `json:"country"`
			City    string `json:"city"`
			Server  string `json:"server"`
			ASN     string `json:"asn"`
			ASNName string `json:"asnname"`
		} `json:"page"`
		Lists struct {
			IPs     []string `json:"ips"`
			Domains []string `json:"domains"`
		} `json:"lists"`
		Stats struct {
			Requests int `json:"requests"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode result response: %w", err)
	}

	return &ScanDetail{
		UUID:     uuid,
		URL:      resp.Page.URL,
		Domain:   resp.Page.Domain,
		Title:    resp.Page.Title,
		IP:       resp.Page.IP,
		Country:  resp.Page.Country,
		City:     resp.Page.City,
		Server:   resp.Page.Server,
		ASN:      resp.Page.ASN,
		ASNName:  resp.Page.ASNName,
		IPs:      resp.Lists.IPs,
		Domains:  resp.Lists.Domains,
		Requests: resp.Stats.Requests,
	}, nil
}

// Get fetches url and returns the response body. It paces and retries according
// to the client's settings. The caller owns nothing extra; the body is read
// fully and closed here.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, url)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", url, lastErr)
}

func (c *Client) do(ctx context.Context, url string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	if c.Rate <= 0 {
		return
	}
	if wait := c.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}
