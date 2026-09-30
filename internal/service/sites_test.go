package service

import "testing"

func TestTrafficRecord(t *testing.T) {
	for _, tc := range []struct {
		s          *SiteService
		kind, want string
	}{
		{&SiteService{BaseURL: "https://sites.example.com"}, "CNAME", "docs.sites.example.com"},
		{&SiteService{BaseURL: "https://sites.example.com", DNSTarget: "edge.example.net"}, "CNAME", "edge.example.net"},
		{&SiteService{BaseURL: "http://localhost:8081", PanelHost: "127.0.0.1"}, "A", "127.0.0.1"},
		{&SiteService{BaseURL: "http://localhost:8081", PanelHost: "::1"}, "AAAA", "::1"},
		{&SiteService{BaseURL: "http://localhost:8081"}, "A", ""},
		{&SiteService{BaseURL: "http://localhost:8081", PanelHost: "panel.example.com"}, "CNAME", "panel.example.com"},
	} {
		if k, v := tc.s.TrafficRecord("docs"); k != tc.kind || v != tc.want {
			t.Errorf("%s/%s/%s: %s %s", tc.s.BaseURL, tc.s.DNSTarget, tc.s.PanelHost, k, v)
		}
	}
}

func TestNormalizeDomainAndSlug(t *testing.T) {
	for in, want := range map[string]string{"WWW.Example.com.": "www.example.com", "bücher.example": "xn--bcher-kva.example", " a.io ": "a.io"} {
		if got, err := NormalizeDomain(in); err != nil || got != want {
			t.Errorf("%q = %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"example", "http://a.com", "a.com/x", "10.0.0.1", "host.local", "x.localhost", "a_b.com", "-a.com", "a.123"} {
		if _, err := NormalizeDomain(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	for in, want := range map[string]string{"My Docs!": "my-docs", "API": "site-api", "a": "site-a", "Über Bot 2": "ber-bot-2"} {
		if got := slugFrom(in); got != want {
			t.Errorf("slugFrom(%q) = %q want %q", in, got, want)
		}
	}
}
