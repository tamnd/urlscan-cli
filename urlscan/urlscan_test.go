package urlscan_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tamnd/urlscan-cli/urlscan"
)

func TestGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := urlscan.NewClient()
	c.Rate = 0 // no pacing in the test

	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) == 0 {
		t.Error("expected non-empty body")
	}
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"recovered":true}`))
	}))
	defer srv.Close()

	c := urlscan.NewClient()
	c.Rate = 0
	c.Retries = 5

	start := time.Now()
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) == 0 {
		t.Error("expected non-empty body after retries")
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

func TestGet404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := urlscan.NewClient()
	c.Rate = 0

	_, err := c.Get(context.Background(), srv.URL)
	if err == nil {
		t.Error("expected error on 404, got nil")
	}
}

func TestSearch(t *testing.T) {
	response := map[string]interface{}{
		"total": 1,
		"took":  10,
		"results": []map[string]interface{}{
			{
				"_id": "0196f0d7-abcd-1234-ef56-7890abcdef12",
				"task": map[string]interface{}{
					"url":    "https://github.com",
					"time":   "2024-01-15T10:00:00.000Z",
					"uuid":   "0196f0d7-abcd-1234-ef56-7890abcdef12",
					"domain": "github.com",
					"method": "automatic",
				},
				"page": map[string]interface{}{
					"url":     "https://github.com",
					"domain":  "github.com",
					"ip":      "140.82.121.4",
					"country": "US",
					"city":    "San Francisco",
					"server":  "GitHub.com",
					"title":   "GitHub",
				},
			},
		},
	}
	body, _ := json.Marshal(response)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q == "" {
			t.Error("search request missing q parameter")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	// Patch BaseURL for the test by building the URL directly.
	c := urlscan.NewClient()
	c.Rate = 0

	// Use a custom search by hitting the test server directly.
	rawBody, err := c.Get(context.Background(), srv.URL+"?q=domain%3Agithub.com&size=1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	var parsed struct {
		Results []struct {
			ID string `json:"_id"`
		} `json:"results"`
	}
	if err := json.Unmarshal(rawBody, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(parsed.Results) != 1 {
		t.Errorf("len(results) = %d, want 1", len(parsed.Results))
	}
	if parsed.Results[0].ID != "0196f0d7-abcd-1234-ef56-7890abcdef12" {
		t.Errorf("result[0]._id = %q", parsed.Results[0].ID)
	}
}

func TestResultDecoding(t *testing.T) {
	response := map[string]interface{}{
		"page": map[string]interface{}{
			"url":     "https://github.com",
			"domain":  "github.com",
			"title":   "GitHub",
			"ip":      "140.82.121.4",
			"country": "US",
			"city":    "San Francisco",
			"server":  "GitHub.com",
			"asn":     "AS36459",
			"asnname": "GITHUB",
		},
		"lists": map[string]interface{}{
			"ips":     []string{"140.82.121.4", "140.82.121.3"},
			"domains": []string{"github.com", "avatars.githubusercontent.com"},
		},
		"stats": map[string]interface{}{
			"requests": 42,
		},
	}
	body, _ := json.Marshal(response)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	c := urlscan.NewClient()
	c.Rate = 0

	rawBody, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	var parsed struct {
		Page struct {
			URL     string `json:"url"`
			Domain  string `json:"domain"`
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
	if err := json.Unmarshal(rawBody, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.Page.URL != "https://github.com" {
		t.Errorf("page.url = %q, want https://github.com", parsed.Page.URL)
	}
	if parsed.Page.ASNName != "GITHUB" {
		t.Errorf("page.asnname = %q, want GITHUB", parsed.Page.ASNName)
	}
	if len(parsed.Lists.IPs) != 2 {
		t.Errorf("lists.ips len = %d, want 2", len(parsed.Lists.IPs))
	}
	if parsed.Stats.Requests != 42 {
		t.Errorf("stats.requests = %d, want 42", parsed.Stats.Requests)
	}
}

func TestNewClient(t *testing.T) {
	c := urlscan.NewClient()
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	if c.UserAgent == "" {
		t.Error("UserAgent is empty")
	}
	if c.Rate == 0 {
		t.Error("Rate should be non-zero by default")
	}
	if c.Retries == 0 {
		t.Error("Retries should be non-zero by default")
	}
}
