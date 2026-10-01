package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/net/html"
)

type SearchConfig struct {
	BaseURL                         string
	Keys                            []string
	Results                         int
	Language, Categories, TimeRange string
	SafeSearch                      int
}
type SearchResult struct {
	Title, URL, Snippet, Published string
	RetrievedMS                    int64
}
type SearchResponse struct {
	Answer  string
	Results []SearchResult
}

type Research struct {
	HTTP     *http.Client
	Resolver *net.Resolver
	next     atomic.Uint64
}

func (r *Research) resolver() *net.Resolver {
	if r != nil && r.Resolver != nil {
		return r.Resolver
	}
	return net.DefaultResolver
}
func publicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	// Provider metadata endpoints are link-local on the major clouds, but keep
	// explicit defense for unusual resolver representations.
	return !ip.Equal(net.ParseIP("169.254.169.254")) && !ip.Equal(net.ParseIP("100.100.100.200"))
}
func (r *Research) ValidateURL(ctx context.Context, raw string) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "http" && u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return nil, errors.New("only public HTTP(S) URLs without credentials are allowed")
	}
	ips, e := r.resolver().LookupIP(ctx, "ip", u.Hostname())
	if e != nil {
		return nil, errors.New("the host could not be resolved")
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return nil, errors.New("private, local, multicast, and metadata addresses are blocked")
		}
	}
	return u, nil
}

func (r *Research) Search(ctx context.Context, cfg SearchConfig, q string) (SearchResponse, error) {
	if strings.TrimSpace(q) == "" || len(q) > 500 {
		return SearchResponse{}, errors.New("search query is invalid")
	}
	base, e := r.ValidateURL(ctx, cfg.BaseURL)
	if e != nil {
		return SearchResponse{}, e
	}
	u := base.ResolveReference(&url.URL{Path: "/search"})
	v := u.Query()
	v.Set("q", q)
	v.Set("format", "json")
	if cfg.Language != "" {
		v.Set("language", cfg.Language)
	}
	if cfg.Categories != "" {
		v.Set("categories", cfg.Categories)
	}
	if cfg.TimeRange != "" {
		v.Set("time_range", cfg.TimeRange)
	}
	v.Set("safesearch", fmt.Sprint(cfg.SafeSearch))
	u.RawQuery = v.Encode()
	limit := cfg.Results
	if limit < 1 || limit > 10 {
		limit = 5
	}
	var last error
	attempts := len(cfg.Keys)
	if attempts == 0 {
		attempts = 1
	}
	start := int(r.next.Add(1) - 1)
	for i := 0; i < attempts; i++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		req.Header.Set("Accept", "application/json")
		if len(cfg.Keys) > 0 {
			req.Header.Set("X-API-Key", cfg.Keys[(start+i)%len(cfg.Keys)])
		}
		resp, e := r.httpClient().Do(req)
		if e != nil {
			return SearchResponse{}, e
		}
		if resp.StatusCode == 429 {
			resp.Body.Close()
			last = errors.New("search service rate limited every configured key")
			continue
		}
		if resp.StatusCode/100 != 2 {
			resp.Body.Close()
			return SearchResponse{}, fmt.Errorf("search service returned %d", resp.StatusCode)
		}
		var raw struct {
			Answers []string                                              `json:"answers"`
			Results []struct{ Title, URL, Content, PublishedDate string } `json:"results"`
		}
		e = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&raw)
		resp.Body.Close()
		if e != nil {
			return SearchResponse{}, e
		}
		out := SearchResponse{}
		if len(raw.Answers) > 0 {
			out.Answer = raw.Answers[0]
		}
		now := time.Now().UnixMilli()
		for _, x := range raw.Results {
			if len(out.Results) >= limit {
				break
			}
			if _, e := r.ValidateURL(ctx, x.URL); e != nil {
				continue
			}
			out.Results = append(out.Results, SearchResult{Title: x.Title, URL: x.URL, Snippet: clip(x.Content, 1200), Published: x.PublishedDate, RetrievedMS: now})
		}
		return out, nil
	}
	return SearchResponse{}, last
}

func (r *Research) httpClient() *http.Client {
	if r != nil && r.HTTP != nil {
		return r.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

// Fetch extracts visible text. Redirect destinations are resolved and checked
// again, preventing redirects and DNS rebinding from reaching internal hosts.
func (r *Research) Fetch(ctx context.Context, raw string) (title, text string, err error) {
	if _, err = r.ValidateURL(ctx, raw); err != nil {
		return
	}
	base := r.httpClient()
	client := *base
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		_, e := r.ValidateURL(req.Context(), req.URL.String())
		return e
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	req.Header.Set("Accept", "text/html,application/json,text/plain;q=0.9")
	req.Header.Set("User-Agent", "BotForge-Research/1.0")
	resp, e := client.Do(req)
	if e != nil {
		return "", "", e
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", "", fmt.Errorf("page returned %d", resp.StatusCode)
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(ct, "text/") && !strings.Contains(ct, "json") && !strings.Contains(ct, "html") {
		return "", "", errors.New("binary pages are not supported")
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, 2<<20+1))
	if e != nil {
		return "", "", e
	}
	if len(b) > 2<<20 {
		return "", "", errors.New("page is too large")
	}
	if strings.Contains(ct, "html") {
		title, text = htmlText(b)
	} else {
		text = string(b)
	}
	return clip(title, 300), clip(text, 20000), nil
}

func htmlText(b []byte) (string, string) {
	n, e := html.Parse(strings.NewReader(string(b)))
	if e != nil {
		return "", string(b)
	}
	var title string
	var parts []string
	var walk func(*html.Node, bool)
	walk = func(x *html.Node, blocked bool) {
		if x.Type == html.ElementNode {
			switch strings.ToLower(x.Data) {
			case "script", "style", "nav", "form", "noscript", "svg":
				blocked = true
			case "title":
				if x.FirstChild != nil {
					title = x.FirstChild.Data
				}
			}
		}
		if x.Type == html.TextNode && !blocked {
			v := strings.Join(strings.Fields(x.Data), " ")
			if v != "" {
				parts = append(parts, v)
			}
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c, blocked)
		}
	}
	walk(n, false)
	return title, strings.Join(parts, "\n")
}
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
