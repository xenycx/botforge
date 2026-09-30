package api

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"

	"botpanel/internal/domain"
)

// auditRoute names what a changing request did. The target is a name taken
// from the route or query (a variable name, file path, backup id), never a
// value or file content.
type auditRoute struct {
	action string
	target func(c fiber.Ctx) string
}

func param(n string) func(fiber.Ctx) string { return func(c fiber.Ctx) string { return c.Params(n) } }
func query(n string) func(fiber.Ctx) string { return func(c fiber.Ctx) string { return c.Query(n) } }

var auditRoutes = map[string]auditRoute{
	"POST /api/v1/bots":                          {"bot.create", nil},
	"PATCH /api/v1/bots/:id":                     {"bot.update", nil},
	"DELETE /api/v1/bots/:id":                    {"bot.delete", nil},
	"POST /api/v1/bots/:id/start":                {"bot.start", nil},
	"POST /api/v1/bots/:id/stop":                 {"bot.stop", nil},
	"POST /api/v1/bots/:id/restart":              {"bot.restart", nil},
	"POST /api/v1/bots/:id/kill":                 {"bot.kill", nil},
	"PUT /api/v1/bots/:id/ports":                 {"bot.ports", nil},
	"PUT /api/v1/bots/:id/env":                   {"env.set", envNames},
	"POST /api/v1/bots/:id/env/:name/reveal":     {"env.reveal", param("name")},
	"DELETE /api/v1/bots/:id/env/:name":          {"env.delete", param("name")},
	"PUT /api/v1/bots/:id/files/content":         {"files.write", query("path")},
	"DELETE /api/v1/bots/:id/files":              {"files.delete", query("path")},
	"POST /api/v1/bots/:id/files/mkdir":          {"files.mkdir", nil},
	"POST /api/v1/bots/:id/files/move":           {"files.move", nil},
	"POST /api/v1/bots/:id/files/extract":        {"files.extract", query("path")},
	"PUT /api/v1/bots/:id/packages":              {"packages.edit", nil},
	"PUT /api/v1/bots/:id/github":                {"deploy.link", nil},
	"DELETE /api/v1/bots/:id/github":             {"deploy.unlink", nil},
	"POST /api/v1/bots/:id/github/deploy":        {"deploy.start", nil},
	"POST /api/v1/bots/:id/backups":              {"backup.create", nil},
	"POST /api/v1/bots/:id/backups/:bid/restore": {"backup.restore", param("bid")},
	"DELETE /api/v1/bots/:id/backups/:bid":       {"backup.delete", param("bid")},
	"PATCH /api/v1/bots/:id/backups/:bid":        {"backup.label", param("bid")},
	"PUT /api/v1/bots/:id/users":                 {"access.grant", nil},
	"POST /api/v1/bots/:id/invites":              {"access.invite", nil},
	"DELETE /api/v1/bots/:id/invites/:iid":       {"access.invite_revoke", param("iid")},
	"POST /api/v1/invites/accept":                {"access.invite_accept", nil},
	"DELETE /api/v1/bots/:id/users/:uid":         {"access.revoke", param("uid")},
	"POST /api/v1/bots/:id/telemetry-key":        {"telemetry.key", nil},
	"DELETE /api/v1/bots/:id/telemetry-key":      {"telemetry.revoke", nil},
	"POST /api/v1/bots/:id/schedules":            {"schedule.create", nil},
	"PATCH /api/v1/bots/:id/schedules/:sid":      {"schedule.update", param("sid")},
	"DELETE /api/v1/bots/:id/schedules/:sid":     {"schedule.delete", param("sid")},
	"POST /api/v1/bots/:id/transfer":             {"bot.transfer", nil},
	"PUT /api/v1/bots/:id/alerts":                {"bot.alerts", nil},
	"PUT /api/v1/bots/:id/tags":                  {"bot.tags", nil},
	"POST /api/v1/bots/batch":                    {"bot.batch", nil},
	"POST /api/v1/me/password":                   {"account.password", nil},
	"DELETE /api/v1/me/sessions/:sid":            {"account.session_revoke", nil},
	"POST /api/v1/me/sessions/revoke-others":     {"account.sessions_revoke", nil},
	"POST /api/v1/me/api-keys":                   {"account.key_create", nil},
	"DELETE /api/v1/me/api-keys/:id":             {"account.key_delete", nil},
	"POST /api/v1/me/tokens":                     {"account.token_create", nil},
	"DELETE /api/v1/me/tokens/:id":               {"account.token_delete", nil},
	"DELETE /api/v1/me/connections/:provider":    {"account.disconnect", param("provider")},
	"POST /api/v1/me/mfa/enable":                 {"account.mfa_enable", nil},
	"POST /api/v1/me/mfa/disable":                {"account.mfa_disable", nil},
	"POST /api/v1/users":                         {"admin.user_create", nil},
	"PATCH /api/v1/users/:id":                    {"admin.user_update", param("id")},
	"PUT /api/v1/admin/settings":                 {"admin.settings", nil},
	"POST /api/v1/auth/logout":                   {"account.logout", nil},
	// Automation API: the target names the token, so the record shows which
	// credential acted.
	"POST /api/v1/automation/bots/:id/start":   {"bot.start", viaToken},
	"POST /api/v1/automation/bots/:id/stop":    {"bot.stop", viaToken},
	"POST /api/v1/automation/bots/:id/restart": {"bot.restart", viaToken},
	"POST /api/v1/automation/bots/:id/deploy":  {"deploy.start", viaToken},
	"POST /api/v1/automation/bots/:id/backups": {"backup.create", viaToken},
}

func viaToken(c fiber.Ctx) string { return "token: " + currentAutoToken(c).Name }

// envNames lists the variable NAMES of a PUT /env body (never the values).
func envNames(c fiber.Ctx) string {
	var in struct {
		Vars map[string]json.RawMessage `json:"vars"`
	}
	if json.Unmarshal(c.Body(), &in) != nil {
		return ""
	}
	names := make([]string, 0, len(in.Vars))
	for n := range in.Vars {
		names = append(names, n)
	}
	s := strings.Join(names, ", ")
	if len(s) > 300 {
		s = s[:297] + "..."
	}
	return s
}

// auditMW records every changing request after it ran (success and refusal;
// server errors are recorded as failed). Reads are not recorded, except the
// explicit secret reveal, which is a POST.
func (s *server) auditMW(c fiber.Ctx) error {
	err := c.Next()
	if s.audit == nil || isSafeMethod(c.Method()) {
		return err
	}
	r, ok := auditRoutes[c.Method()+" "+c.Route().Path]
	if !ok {
		return err
	}
	status := c.Response().StatusCode()
	if err != nil {
		status = fiber.StatusInternalServerError
		if fe, ok := err.(*fiber.Error); ok {
			status = fe.Code
		}
		var e errorBody
		_ = e
		if code := statusOfError(err); code != 0 {
			status = code
		}
	}
	outcome := "ok"
	switch {
	case status == fiber.StatusForbidden || status == fiber.StatusNotFound || status == fiber.StatusUnauthorized:
		outcome = "denied"
	case status >= 400:
		outcome = "failed"
	}
	if outcome == "denied" && !strings.HasPrefix(r.action, "env.") && !strings.HasPrefix(r.action, "admin.") && r.action != "access.grant" {
		return err // only security-relevant refusals are worth keeping
	}
	u := currentUser(c)
	ev := domain.AuditEvent{Action: r.action, Outcome: outcome}
	ev.ActorID, ev.ActorLabel = strPtr(u.ID), strPtr(u.Email)
	ip := c.IP()
	ev.IP = &ip
	if r.target != nil {
		ev.Target = strPtr(strings.Clone(r.target(c)))
	}
	botID := strings.Clone(c.Params("id"))
	switch {
	case strings.HasPrefix(c.Route().Path, "/api/v1/bots/:id"), strings.HasPrefix(c.Route().Path, "/api/v1/automation/bots/:id"):
		ev.BotID = &botID
		if b, e := s.bots.Store.GetBot(c.Context(), botID); e == nil {
			ev.BotName = strPtr(b.Name)
		}
	case r.action == "access.invite_accept" && outcome == "ok":
		var inv struct {
			BotID   string `json:"bot_id"`
			BotName string `json:"bot_name"`
		}
		if json.Unmarshal(c.Response().Body(), &inv) == nil && inv.BotID != "" {
			ev.BotID, ev.BotName = &inv.BotID, &inv.BotName
		}
	case r.action == "bot.create" && outcome == "ok":
		var created struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if json.Unmarshal(c.Response().Body(), &created) == nil && created.ID != "" {
			ev.BotID, ev.BotName = &created.ID, &created.Name
		}
	case strings.HasPrefix(c.Route().Path, "/api/v1/users/:id"):
		ev.SubjectUserID = &botID // the :id here is a user id
	default:
		ev.SubjectUserID = strPtr(u.ID)
	}
	if r.action == "access.grant" {
		var in struct {
			Email string `json:"email"`
		}
		if json.Unmarshal(c.Body(), &in) == nil {
			ev.Target = strPtr(in.Email)
		}
	}
	s.audit.Record(c.Context(), ev)
	return err
}

// statusOfError mirrors errorHandler's mapping for the audit outcome.
func statusOfError(err error) int {
	var ve *domain.ValidationError
	var be *domain.BusyError
	switch {
	case asErr(err, &ve):
		return fiber.StatusBadRequest
	case asErr(err, &be):
		return fiber.StatusConflict
	case asErr(err, new(*domain.CapacityError)):
		return fiber.StatusConflict
	case isErr(err, domain.ErrNotFound):
		return fiber.StatusNotFound
	case isErr(err, domain.ErrForbidden):
		return fiber.StatusForbidden
	case isErr(err, domain.ErrUnauthorized):
		return fiber.StatusUnauthorized
	case isErr(err, domain.ErrNotStopped), isErr(err, domain.ErrConflict):
		return fiber.StatusConflict
	}
	return 0
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

type auditDTO struct {
	ID      int64   `json:"id"`
	AtMS    int64   `json:"at_ms"`
	Actor   *string `json:"actor"`
	BotID   *string `json:"bot_id"`
	BotName *string `json:"bot_name"`
	Action  string  `json:"action"`
	Target  *string `json:"target"`
	Outcome string  `json:"outcome"`
}

func auditPage(evs []domain.AuditEvent, limit int) fiber.Map {
	out := make([]auditDTO, len(evs))
	for i, e := range evs {
		out[i] = auditDTO{e.ID, e.AtMS, e.ActorLabel, e.BotID, e.BotName, e.Action, e.Target, e.Outcome}
	}
	m := fiber.Map{"events": out}
	if len(evs) == limit {
		m["next_before"] = evs[len(evs)-1].ID
	}
	return m
}

func auditQuery(c fiber.Ctx) (before int64, limit int, err error) {
	limit = 50
	if v := c.Query("limit"); v != "" {
		if limit, err = strconv.Atoi(v); err != nil || limit < 1 || limit > 200 {
			return 0, 0, domain.Invalid("limit must be between 1 and 200")
		}
	}
	if v := c.Query("before"); v != "" {
		if before, err = strconv.ParseInt(v, 10, 64); err != nil || before < 0 {
			return 0, 0, domain.Invalid("before must be an event id")
		}
	}
	return before, limit, nil
}

func (s *server) botAudit(c fiber.Ctx) error {
	before, limit, err := auditQuery(c)
	if err != nil {
		return err
	}
	evs, err := s.audit.ForBot(c.Context(), currentUser(c), strings.Clone(c.Params("id")), before, limit)
	if err != nil {
		return err
	}
	return c.JSON(auditPage(evs, limit))
}

func (s *server) visibleAudit(c fiber.Ctx) error {
	before, limit, err := auditQuery(c)
	if err != nil {
		return err
	}
	evs, err := s.audit.Visible(c.Context(), currentUser(c), before, limit)
	if err != nil {
		return err
	}
	return c.JSON(auditPage(evs, limit))
}
