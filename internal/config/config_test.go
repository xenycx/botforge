package config

import (
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDefaultsProduction(t *testing.T) {
	c, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !c.Production || c.DBPath != "/var/lib/botpanel/botpanel.db" || c.RunnerMode != RunnerLocal {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestDevelopmentUsesRelativePaths(t *testing.T) {
	c, err := Load(env(map[string]string{"BOTPANEL_ENV": "development"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Production || c.DBPath != ".dev-data/botpanel.db" {
		t.Fatalf("unexpected dev config: %+v", c)
	}
}

func TestInvalid(t *testing.T) {
	for name, m := range map[string]map[string]string{
		"env":    {"BOTPANEL_ENV": "staging"},
		"listen": {"BOTPANEL_LISTEN": "nonsense"},
		"port":   {"BOTPANEL_LISTEN": "127.0.0.1:99999"},
		"runner": {"BOTPANEL_RUNNER_MODE": "remote"},
		"conns":  {"BOTPANEL_DB_MAX_CONNS": "0"},
		"dbpath": {"BOTPANEL_DB_PATH": "/x/a?b"},
	} {
		if _, err := Load(env(m)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestContainerPolicyValidation(t *testing.T) {
	for name, m := range map[string]map[string]string{
		"root user":      {"BOTPANEL_CONTAINER_USER": "0:0"},
		"root gid":       {"BOTPANEL_CONTAINER_USER": "1000:0"},
		"non numeric":    {"BOTPANEL_CONTAINER_USER": "nobody"},
		"host network":   {"BOTPANEL_CONTAINER_NETWORK": "host"},
		"joined network": {"BOTPANEL_CONTAINER_NETWORK": "container:abc"},
		"bad owner":      {"BOTPANEL_WORKSPACE_OWNER": "x:y"},
		"workers":        {"BOTPANEL_RUNNER_WORKERS": "0"},
		"build timeout":  {"BOTPANEL_BUILD_TIMEOUT": "1s"},
	} {
		if _, err := Load(env(m)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	c, err := Load(env(map[string]string{"BOTPANEL_CONTAINER_USER": "0:0", "BOTPANEL_ALLOW_ROOT_CONTAINER_USER": "1", "BOTPANEL_WORKSPACE_OWNER": "1000:1000"}))
	if err != nil || !c.AllowRootUser || c.WorkspaceOwner != "1000:1000" {
		t.Fatal(err, c)
	}
	if c, _ := Load(env(nil)); c.ContainerUser != "65532:65532" || c.ContainerNetwork != "bridge" || c.DockerHost != "unix:///var/run/docker.sock" {
		t.Fatalf("defaults: %+v", c)
	}
}

func TestTelemetryConfig(t *testing.T) {
	for name, m := range map[string]map[string]string{
		"too frequent": {"BOTPANEL_TELEMETRY_INTERVAL": "5s"},
		"too rare":     {"BOTPANEL_TELEMETRY_INTERVAL": "5m"},
		"retention":    {"BOTPANEL_TELEMETRY_RETENTION": "1m"},
		"unparsable":   {"BOTPANEL_TELEMETRY_INTERVAL": "soon"},
	} {
		if _, err := Load(env(m)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	c, err := Load(env(nil))
	if err != nil || c.TelemetryInterval != 30*time.Second || c.TelemetryRetention != 7*24*time.Hour {
		t.Fatal(err, c.TelemetryInterval, c.TelemetryRetention)
	}
}

func TestOAuthConfigValidation(t *testing.T) {
	load := func(kv map[string]string) error {
		_, err := Load(func(k string) string { return kv[k] })
		return err
	}
	base := map[string]string{"BOTPANEL_ENV": "development"}
	with := func(extra map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	if err := load(base); err != nil {
		t.Fatal("OAuth must be optional:", err)
	}
	cases := map[string]struct {
		env map[string]string
		ok  bool
	}{
		"id without secret":    {map[string]string{"BOTPANEL_GITHUB_CLIENT_ID": "a"}, false},
		"missing public url":   {map[string]string{"BOTPANEL_GITHUB_CLIENT_ID": "a", "BOTPANEL_GITHUB_CLIENT_SECRET": "b"}, false},
		"path in public url":   {map[string]string{"BOTPANEL_GITHUB_CLIENT_ID": "a", "BOTPANEL_GITHUB_CLIENT_SECRET": "b", "BOTPANEL_PUBLIC_URL": "https://p.example.com/panel"}, false},
		"http non-local":       {map[string]string{"BOTPANEL_GITHUB_CLIENT_ID": "a", "BOTPANEL_GITHUB_CLIENT_SECRET": "b", "BOTPANEL_PUBLIC_URL": "http://p.example.com"}, false},
		"https ok":             {map[string]string{"BOTPANEL_GITHUB_CLIENT_ID": "a", "BOTPANEL_GITHUB_CLIENT_SECRET": "b", "BOTPANEL_PUBLIC_URL": "https://panel.xenyc.ge/"}, true},
		"http localhost (dev)": {map[string]string{"BOTPANEL_DISCORD_CLIENT_ID": "a", "BOTPANEL_DISCORD_CLIENT_SECRET": "b", "BOTPANEL_PUBLIC_URL": "http://localhost:8080"}, true},
	}
	for name, tc := range cases {
		if err := load(with(tc.env)); (err == nil) != tc.ok {
			t.Errorf("%s: err = %v, want ok=%v", name, err, tc.ok)
		}
	}
	c, _ := Load(func(k string) string { return with(cases["https ok"].env)[k] })
	if c.PublicURL != "https://panel.xenyc.ge" || !c.OAuthConfigured("github") || c.OAuthConfigured("discord") {
		t.Fatalf("%+v", c)
	}
}
