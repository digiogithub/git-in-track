package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultRepo is the GitHub repository gintrack releases are published to.
const DefaultRepo = "digiogithub/git-in-track"

const defaultBaseURL = "https://api.github.com"

// Asset is one downloadable file of a release.
type Asset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Release is a GitHub release.
type Release struct {
	TagName    string  `json:"tag_name"`
	Name       string  `json:"name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	HTMLURL    string  `json:"html_url"`
	Body       string  `json:"body"`
	Assets     []Asset `json:"assets"`
}

// Version returns the release version without the v prefix.
func (r Release) Version() string {
	return strings.TrimPrefix(strings.TrimPrefix(r.TagName, "v"), "V")
}

// ErrNotFound is returned when no matching release exists.
var ErrNotFound = errors.New("selfupdate: release not found")

// RateLimitError reports an exhausted GitHub API rate limit.
type RateLimitError struct {
	Reset time.Time // zero when GitHub did not say
}

func (e *RateLimitError) Error() string {
	msg := "selfupdate: GitHub API rate limit exceeded; set GITHUB_TOKEN to raise the limit"
	if !e.Reset.IsZero() {
		msg += " (resets at " + e.Reset.UTC().Format(time.RFC3339) + ")"
	}
	return msg
}

// Options configures a Client. The zero value is usable.
type Options struct {
	BaseURL    string       // GitHub API root; default https://api.github.com
	Repo       string       // owner/name; default DefaultRepo
	HTTPClient *http.Client // default: 30s-header timeout client using proxy from env
	Token      string       // default: $GITHUB_TOKEN
	Version    string       // running gintrack version, for the user agent
	MaxPages   int          // release list page cap; default 10
}

// Client talks to the GitHub releases API and downloads assets.
type Client struct {
	base     string
	repo     string
	hc       *http.Client
	token    string
	ua       string
	maxPages int
}

// New builds a Client from opts, filling defaults.
func New(opts Options) *Client {
	c := &Client{
		base:     strings.TrimRight(opts.BaseURL, "/"),
		repo:     opts.Repo,
		hc:       opts.HTTPClient,
		token:    opts.Token,
		maxPages: opts.MaxPages,
	}
	if c.base == "" {
		c.base = defaultBaseURL
	}
	if c.repo == "" {
		c.repo = DefaultRepo
	}
	if c.token == "" {
		c.token = os.Getenv("GITHUB_TOKEN")
	}
	if c.maxPages <= 0 {
		c.maxPages = 10
	}
	v := opts.Version
	if v == "" {
		v = "dev"
	}
	c.ua = "gintrack/" + v
	if c.hc == nil {
		c.hc = &http.Client{Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		}}
	}
	return c
}

func (c *Client) newRequest(ctx context.Context, rawURL string, api bool) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("selfupdate: %w", err)
	}
	req.Header.Set("User-Agent", c.ua)
	if api {
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
	}
	return req, nil
}

func rateLimited(resp *http.Response) *RateLimitError {
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusTooManyRequests {
		return nil
	}
	if resp.Header.Get("X-RateLimit-Remaining") != "0" {
		return nil
	}
	e := &RateLimitError{}
	if n, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		e.Reset = time.Unix(n, 0)
	}
	return e
}

// getJSON GETs path under the repo API root and decodes the body into out.
func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	req, err := c.newRequest(ctx, c.base+"/repos/"+c.repo+path, true)
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("selfupdate: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if rl := rateLimited(resp); rl != nil {
		return rl
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode != http.StatusOK:
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("selfupdate: GET %s: %s: %s", path, resp.Status, strings.TrimSpace(string(b)))
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(out); err != nil {
		return fmt.Errorf("selfupdate: decode %s: %w", path, err)
	}
	return nil
}

// Latest returns the release with the highest semantic version, skipping
// drafts and, unless includePrerelease is set, pre-releases. The API order is
// not trusted. Tags that are not semver are ignored.
func (c *Client) Latest(ctx context.Context, includePrerelease bool) (*Release, error) {
	var best *Release
	var bestV Semver
	for page := 1; page <= c.maxPages; page++ {
		var rels []Release
		q := url.Values{"per_page": {"100"}, "page": {strconv.Itoa(page)}}
		if err := c.getJSON(ctx, "/releases?"+q.Encode(), &rels); err != nil {
			return nil, err
		}
		for i := range rels {
			r := rels[i]
			if r.Draft {
				continue
			}
			v, err := ParseSemver(r.TagName)
			if err != nil {
				continue
			}
			if (r.Prerelease || v.IsPrerelease()) && !includePrerelease {
				continue
			}
			if best == nil || v.Compare(bestV) > 0 {
				best, bestV = &r, v
			}
		}
		if len(rels) < 100 {
			break
		}
	}
	if best == nil {
		return nil, ErrNotFound
	}
	return best, nil
}

// ByVersion returns the release tagged v{version}, falling back to a tag
// without the v. version may be given with or without a leading v.
func (c *Client) ByVersion(ctx context.Context, version string) (*Release, error) {
	v := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(version), "v"), "V")
	if v == "" || strings.ContainsAny(v, "/?# ") {
		return nil, fmt.Errorf("selfupdate: invalid version %q", version)
	}
	for _, tag := range []string{"v" + v, v} {
		var r Release
		err := c.getJSON(ctx, "/releases/tags/"+url.PathEscape(tag), &r)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if r.Draft {
			continue
		}
		return &r, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrNotFound, version)
}

// ChecksumsName is the checksum manifest asset name.
const ChecksumsName = "checksums.txt"

// AssetName returns the archive name the contract gives for a platform.
func AssetName(version, goos, goarch string) string {
	ext := ".zip"
	if goos == "linux" {
		ext = ".tar.gz"
	}
	return fmt.Sprintf("gintrack_%s_%s_%s%s", version, goos, goarch, ext)
}

// SelectAsset picks the archive for goos/goarch. When absent the error lists
// the assets the release does have.
func SelectAsset(r *Release, goos, goarch string) (Asset, error) {
	want := AssetName(r.Version(), goos, goarch)
	for _, a := range r.Assets {
		if a.Name == want {
			return a, nil
		}
	}
	names := make([]string, 0, len(r.Assets))
	for _, a := range r.Assets {
		names = append(names, a.Name)
	}
	sort.Strings(names)
	return Asset{}, fmt.Errorf("selfupdate: release %s has no asset %q for %s/%s; available: %s",
		r.TagName, want, goos, goarch, strings.Join(names, ", "))
}
