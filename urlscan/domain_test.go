package urlscan

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// These tests are offline: they exercise the URI driver's pure string functions
// and the host wiring (mint, body, resolve), which need no network. The client's
// HTTP behaviour is covered in urlscan_test.go.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "urlscan" {
		t.Errorf("Scheme = %q, want urlscan", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "urlscan" {
		t.Errorf("Identity.Binary = %q, want urlscan", info.Identity.Binary)
	}
}

func TestClassifyUUID(t *testing.T) {
	uuid := "0196f0d7-abcd-1234-ef56-7890abcdef12"
	typ, id, err := Domain{}.Classify(uuid)
	if err != nil {
		t.Fatalf("Classify(%q) error: %v", uuid, err)
	}
	if typ != "uuid" || id != uuid {
		t.Errorf("Classify(%q) = (%q, %q), want (uuid, %q)", uuid, typ, id, uuid)
	}
}

func TestClassifyURL(t *testing.T) {
	u := "https://github.com"
	typ, id, err := Domain{}.Classify(u)
	if err != nil {
		t.Fatalf("Classify(%q) error: %v", u, err)
	}
	if typ != "url" || id != u {
		t.Errorf("Classify(%q) = (%q, %q), want (url, %q)", u, typ, id, u)
	}
}

func TestClassifyQuery(t *testing.T) {
	q := "domain:github.com"
	typ, id, err := Domain{}.Classify(q)
	if err != nil {
		t.Fatalf("Classify(%q) error: %v", q, err)
	}
	if typ != "query" || id != q {
		t.Errorf("Classify(%q) = (%q, %q), want (query, %q)", q, typ, id, q)
	}
}

func TestLocateUUID(t *testing.T) {
	uuid := "0196f0d7-abcd-1234-ef56-7890abcdef12"
	got, err := Domain{}.Locate("uuid", uuid)
	want := "https://" + Host + "/result/" + uuid + "/"
	if err != nil || got != want {
		t.Errorf("Locate(uuid, %q) = (%q, %v), want (%q, nil)", uuid, got, err, want)
	}
}

func TestLocateURL(t *testing.T) {
	u := "https://github.com"
	got, err := Domain{}.Locate("url", u)
	want := "https://" + Host + "/search/#page.url:" + u
	if err != nil || got != want {
		t.Errorf("Locate(url, %q) = (%q, %v), want (%q, nil)", u, got, err, want)
	}
}

func TestLocateQuery(t *testing.T) {
	q := "domain:github.com"
	got, err := Domain{}.Locate("query", q)
	want := "https://" + Host + "/search/#" + q
	if err != nil || got != want {
		t.Errorf("Locate(query, %q) = (%q, %v), want (%q, nil)", q, got, err, want)
	}
}

func TestLocateUnknownType(t *testing.T) {
	_, err := Domain{}.Locate("bogus", "anything")
	if err == nil {
		t.Error("expected error for unknown uriType, got nil")
	}
}

func TestIsUUID(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{"0196f0d7-abcd-1234-ef56-7890abcdef12", true},
		{"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", true},
		{"not-a-uuid", false},
		{"domain:github.com", false},
		{"https://github.com", false},
		{"", false},
		// wrong length
		{"0196f0d7-abcd-1234-ef56-7890abcdef1", false},
	}
	for _, tc := range cases {
		got := isUUID(tc.s)
		if got != tc.want {
			t.Errorf("isUUID(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

func TestHostWiring(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}

	// Mint a ScanDetail to a URI (it is registered via the result resolver op).
	sd := &ScanDetail{
		UUID:   "0196f0d7-abcd-1234-ef56-7890abcdef12",
		URL:    "https://github.com",
		Domain: "github.com",
		IP:     "140.82.121.4",
	}
	u, err := h.Mint(sd)
	if err != nil {
		t.Fatalf("Mint ScanDetail: %v", err)
	}
	if u.Scheme != "urlscan" {
		t.Errorf("Mint scheme = %q, want urlscan", u.Scheme)
	}

	// ResolveOn with a UUID should produce a urlscan:// URI.
	uuid := "0196f0d7-abcd-1234-ef56-7890abcdef12"
	got, err := h.ResolveOn("urlscan", uuid)
	if err != nil {
		t.Fatalf("ResolveOn(%q): %v", uuid, err)
	}
	if got.Scheme != "urlscan" {
		t.Errorf("ResolveOn scheme = %q, want urlscan", got.Scheme)
	}
}
