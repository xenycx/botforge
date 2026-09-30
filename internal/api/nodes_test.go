package api

import (
	"encoding/json"
	"testing"

	"botpanel/internal/domain"
)

func seedSamples(e *env, n int) {
	for i := 1; i <= n; i++ {
		e.db.InsertTelemetry(nil2ctx(), domain.Telemetry{NodeID: domain.LocalNodeID, SampledAtMS: int64(i) * 1000, CPUPercent: float64(i),
			LogicalCPUs: 4, MemoryUsedBytes: 1, MemoryTotalBytes: 2, DiskUsedBytes: 1, DiskTotalBytes: 2, RunningBots: i})
	}
}

func TestNodeAndTelemetryEndpointsAreAdminOnly(t *testing.T) {
	e := newEnv(t)
	user := e.user("u@example.com", domain.RoleUser)
	admin := e.user("a@example.com", domain.RoleAdmin)
	for _, p := range []string{"/api/v1/nodes", "/api/v1/nodes/" + domain.LocalNodeID + "/telemetry"} {
		user.mustStatus(403, "GET", p, nil)
		anon := &client{e: e}
		if resp, _ := anon.do("GET", p, nil); resp.StatusCode != 401 {
			t.Fatalf("%s anonymous: %d", p, resp.StatusCode)
		}
	}
	seedSamples(e, 5)
	var nodes struct {
		Nodes []struct {
			ID     string `json:"id"`
			Latest *struct {
				CPU  float64 `json:"cpu_percent"`
				Bots int     `json:"running_bots"`
			} `json:"latest"`
		} `json:"nodes"`
	}
	json.Unmarshal(admin.mustStatus(200, "GET", "/api/v1/nodes", nil), &nodes)
	if len(nodes.Nodes) != 1 || nodes.Nodes[0].ID != domain.LocalNodeID || nodes.Nodes[0].Latest == nil || nodes.Nodes[0].Latest.Bots != 5 {
		t.Fatalf("%+v", nodes)
	}
}

func TestTelemetryQueryParametersAreValidatedAndBounded(t *testing.T) {
	e := newEnv(t)
	admin := e.user("a@example.com", domain.RoleAdmin)
	seedSamples(e, 10)
	base := "/api/v1/nodes/" + domain.LocalNodeID + "/telemetry"
	var out struct {
		Samples []struct {
			At int64 `json:"sampled_at_ms"`
		} `json:"samples"`
	}
	json.Unmarshal(admin.mustStatus(200, "GET", base+"?limit=3&since_ms=2000", nil), &out)
	if len(out.Samples) != 3 || out.Samples[0].At != 8000 || out.Samples[2].At != 10000 {
		t.Fatalf("%+v", out)
	}
	for _, q := range []string{"?limit=0", "?limit=100000", "?limit=x", "?since_ms=-1", "?since_ms=abc"} {
		admin.mustStatus(400, "GET", base+q, nil)
	}
	admin.mustStatus(404, "GET", "/api/v1/nodes/00000000-0000-4000-8000-000000000000/telemetry", nil)
}
