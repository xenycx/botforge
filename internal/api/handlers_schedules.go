package api

import (
	"strings"

	"github.com/gofiber/fiber/v3"

	"botpanel/internal/service"
)

type scheduleDTO struct {
	ID          string  `json:"id"`
	Action      string  `json:"action"`
	Spec        string  `json:"spec"`
	Timezone    string  `json:"timezone"`
	Enabled     bool    `json:"enabled"`
	OwnerEmail  string  `json:"owner_email"`
	NextRunMS   *int64  `json:"next_run_at_ms"`
	LastRunMS   *int64  `json:"last_run_at_ms"`
	LastStatus  *string `json:"last_status"`
	LastMessage *string `json:"last_message"`
	Upcoming    []int64 `json:"upcoming"`
	CanEdit     bool    `json:"can_edit"`
	CreatedAtMS int64   `json:"created_at_ms"`
}

func toSchedule(v service.ScheduleView) scheduleDTO {
	up := v.Upcoming
	if up == nil {
		up = []int64{}
	}
	next := v.NextRunMS
	if !v.Enabled {
		next = nil
	}
	return scheduleDTO{v.ID, v.Action, v.Spec, v.Timezone, v.Enabled, v.OwnerEmail, next, v.LastRunMS, v.LastStatus,
		v.LastMessage, up, v.CanEdit, v.CreatedAtMS}
}

type scheduleBody struct {
	Action   *string `json:"action"`
	Spec     *string `json:"spec"`
	Timezone *string `json:"timezone"`
	Enabled  *bool   `json:"enabled"`
}

func (b scheduleBody) input() service.ScheduleInput {
	return service.ScheduleInput{Action: b.Action, Spec: b.Spec, Timezone: b.Timezone, Enabled: b.Enabled}
}

func (s *server) listSchedules(c fiber.Ctx) error {
	vs, err := s.schedules.List(c.Context(), currentUser(c), strings.Clone(c.Params("id")))
	if err != nil {
		return err
	}
	out := make([]scheduleDTO, len(vs))
	for i, v := range vs {
		out[i] = toSchedule(v)
	}
	return c.JSON(fiber.Map{"schedules": out})
}

func (s *server) createSchedule(c fiber.Ctx) error {
	var in scheduleBody
	if err := decode(c, &in); err != nil {
		return err
	}
	v, err := s.schedules.Create(c.Context(), currentUser(c), strings.Clone(c.Params("id")), in.input())
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(toSchedule(v))
}

func (s *server) patchSchedule(c fiber.Ctx) error {
	var in scheduleBody
	if err := decode(c, &in); err != nil {
		return err
	}
	v, err := s.schedules.Update(c.Context(), currentUser(c), strings.Clone(c.Params("id")), strings.Clone(c.Params("sid")), in.input())
	if err != nil {
		return err
	}
	return c.JSON(toSchedule(v))
}

func (s *server) deleteSchedule(c fiber.Ctx) error {
	if err := s.schedules.Delete(c.Context(), currentUser(c), strings.Clone(c.Params("id")), strings.Clone(c.Params("sid"))); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *server) runSchedule(c fiber.Ctx) error {
	status, msg, err := s.schedules.RunNow(c.Context(), currentUser(c), strings.Clone(c.Params("id")), strings.Clone(c.Params("sid")))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"status": status, "message": msg})
}

func (s *server) previewSchedule(c fiber.Ctx) error {
	times, err := s.schedules.Preview(c.Query("spec"), c.Query("timezone"), 5)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"upcoming": times})
}
