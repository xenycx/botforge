package api

import (
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"

	"botpanel/internal/addons"
	"botpanel/internal/service"
)

func serviceAddon(in addonIn) service.AddonInput {
	return service.AddonInput{Kind: in.Kind, MemoryBytes: in.MemoryBytes}
}

// listAddonKinds returns the add-on catalog and whether this panel can run it.
func (s *server) listAddonKinds(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"addons": addons.List(), "available": s.bots.AddonData != nil})
}

func (s *server) listBotAddons(c fiber.Ctx) error {
	as, err := s.bots.Addons(c.Context(), currentUser(c), strings.Clone(c.Params("id")))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"addons": as})
}

func (s *server) addBotAddon(c fiber.Ctx) error {
	var in addonIn
	if err := decode(c, &in); err != nil {
		return err
	}
	v, err := s.bots.AddAddon(c.Context(), currentUser(c), strings.Clone(c.Params("id")), serviceAddon(in))
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(v)
}

func (s *server) patchBotAddon(c fiber.Ctx) error {
	var in struct {
		MemoryBytes int64 `json:"memory_bytes"`
	}
	if err := decode(c, &in); err != nil {
		return err
	}
	if err := s.bots.SetAddonMemory(c.Context(), currentUser(c), strings.Clone(c.Params("id")), strings.Clone(c.Params("kind")), in.MemoryBytes); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *server) deleteBotAddon(c fiber.Ctx) error {
	if err := s.bots.RemoveAddon(c.Context(), currentUser(c), strings.Clone(c.Params("id")), strings.Clone(c.Params("kind"))); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// revealBotAddon returns the connection variables (including the password).
func (s *server) revealBotAddon(c fiber.Ctx) error {
	vars, err := s.bots.AddonConnection(c.Context(), currentUser(c), strings.Clone(c.Params("id")), strings.Clone(c.Params("kind")))
	if err != nil {
		return err
	}
	c.Set("Cache-Control", "no-store")
	return c.JSON(fiber.Map{"variables": vars})
}

func (s *server) botAddonLogs(c fiber.Ctx) error {
	n, _ := strconv.Atoi(c.Query("lines", "200"))
	out, err := s.bots.AddonLogs(c.Context(), currentUser(c), strings.Clone(c.Params("id")), strings.Clone(c.Params("kind")), n)
	if err != nil {
		return err
	}
	c.Set("Cache-Control", "no-store")
	return c.JSON(fiber.Map{"output": out})
}
