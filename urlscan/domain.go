package urlscan

import (
	"context"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes urlscan.io as a kit Domain: a driver that a multi-domain
// host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/urlscan-cli/urlscan"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then dereferences
// urlscan:// URIs by routing to the operations Register installs. The same
// Domain also builds the standalone urlscan binary (see cli.NewApp), so the
// binary and a host share one source of truth.
func init() { kit.Register(Domain{}) }

// Domain is the urlscan.io driver. It carries no state; the per-run client is
// built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against, and
// the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "urlscan",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "urlscan",
			Short:  "Read public urlscan.io scan data",
			Long: `A command line for urlscan.io.

urlscan reads public scan data from urlscan.io over HTTPS, shapes it into
clean records, and prints output that pipes into the rest of your tools. No API
key required for search and result lookups.`,
			Site: Host,
			Repo: "https://github.com/tamnd/urlscan-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	// search: query urlscan.io for matching scans.
	kit.Handle(app, kit.OpMeta{Name: "search", Group: "read", List: true,
		Summary: "Search urlscan.io scans",
		Args:    []kit.Arg{{Name: "query", Help: "search query e.g. 'domain:github.com' or 'page.ip:8.8.8.8'"}}}, doSearch)

	// result: fetch the full detail for a single scan by UUID.
	kit.Handle(app, kit.OpMeta{Name: "result", Group: "read", Single: true,
		Summary: "Fetch a scan result by UUID", URIType: "uuid", Resolver: true,
		Args: []kit.Arg{{Name: "uuid", Help: "scan UUID"}}}, doResult)
}

// newClient builds the client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := NewClient()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.HTTP.Timeout = cfg.Timeout
	}
	return c, nil
}

// --- inputs ---

type searchInput struct {
	Query  string  `kit:"arg" help:"search query e.g. 'domain:github.com' or 'page.ip:8.8.8.8'"`
	Size   int     `kit:"flag,inherit" help:"max results" default:"10"`
	Client *Client `kit:"inject"`
}

type resultInput struct {
	UUID   string  `kit:"arg" help:"scan UUID"`
	Client *Client `kit:"inject"`
}

// --- handlers ---

func doSearch(ctx context.Context, in searchInput, emit func(*ScanResult) error) error {
	results, err := in.Client.Search(ctx, in.Query, in.Size)
	if err != nil {
		return mapErr(err)
	}
	for _, r := range results {
		if err := emit(r); err != nil {
			return err
		}
	}
	return nil
}

func doResult(ctx context.Context, in resultInput, emit func(*ScanDetail) error) error {
	detail, err := in.Client.Result(ctx, in.UUID)
	if err != nil {
		return mapErr(err)
	}
	return emit(detail)
}

// --- Resolver: the URI-native string functions, pure and network-free ---

// Classify turns any accepted input into the canonical (type, id).
// A 36-char UUID-like string (with hyphens) maps to "uuid"; a URL maps to "url";
// everything else is treated as a search query.
func (Domain) Classify(input string) (uriType, id string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", errs.Usage("empty urlscan reference")
	}
	if isUUID(input) {
		return "uuid", input, nil
	}
	if isURL(input) {
		return "url", input, nil
	}
	return "query", input, nil
}

// Locate is the inverse: the live https URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	switch uriType {
	case "uuid":
		return "https://" + Host + "/result/" + id + "/", nil
	case "url":
		return "https://" + Host + "/search/#page.url:" + id, nil
	case "query":
		return "https://" + Host + "/search/#" + id, nil
	default:
		return "", errs.Usage("urlscan has no resource type %q", uriType)
	}
}

// --- helpers ---

// isUUID returns true if s looks like a standard 36-character UUID
// (8-4-4-4-12 hex groups separated by hyphens).
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

// isURL returns true if s looks like an http/https URL.
func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// mapErr converts a library error into the kit error kind that carries the right
// exit code.
func mapErr(err error) error {
	return err
}
