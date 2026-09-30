package api

import (
	"strings"

	"github.com/gofiber/fiber/v3"

	"botpanel/internal/domain"
)

type alertPrefsDTO struct {
	Crash          bool `json:"crash"`
	Deploy         bool `json:"deploy"`
	Backup         bool `json:"backup"`
	Recovery       bool `json:"recovery"`
	HeartbeatAfter int  `json:"heartbeat_after_s"`
}

func toPrefs(p domain.AlertPrefs) alertPrefsDTO {
	return alertPrefsDTO{p.Crash, p.Deploy, p.Backup, p.Recovery, p.HeartbeatAfter}
}

func (s *server) getHealth(c fiber.Ctx) error {
	v, err := s.health.Get(c.Context(), currentUser(c), strings.Clone(c.Params("id")))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"state": v.State, "last_seen_at_ms": v.LastSeenAtMS, "ready": v.Ready,
		"alerts": toPrefs(v.Prefs), "webhook": v.Webhook})
}

func (s *server) putAlerts(c fiber.Ctx) error {
	var in alertPrefsDTO
	if err := decode(c, &in); err != nil {
		return err
	}
	p, err := s.health.SetPrefs(c.Context(), currentUser(c), strings.Clone(c.Params("id")),
		domain.AlertPrefs{Crash: in.Crash, Deploy: in.Deploy, Backup: in.Backup, Recovery: in.Recovery, HeartbeatAfter: in.HeartbeatAfter})
	if err != nil {
		return err
	}
	return c.JSON(toPrefs(p))
}

func (s *server) testAlert(c fiber.Ctx) error {
	if err := s.health.Test(c.Context(), currentUser(c), strings.Clone(c.Params("id"))); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *server) capacity(c fiber.Ctx) error {
	u := currentUser(c)
	cp, err := s.bots.Capacity(c.Context(), u)
	if err != nil {
		return err
	}
	out := fiber.Map{"bots": cp.Bots, "max_bots": cp.MaxBots, "memory_bytes": cp.Memory, "max_memory_bytes": cp.MaxMemory,
		"exempt": cp.Exempt, "build_memory_bytes": s.buildMemory}
	if u.IsAdmin() {
		out["node"] = fiber.Map{"running": cp.NodeRunning, "reserved_bytes": cp.NodeReserved, "budget_bytes": cp.NodeBudget}
	}
	return c.JSON(out)
}
