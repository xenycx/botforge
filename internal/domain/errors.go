package domain

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	// ErrNotStopped rejects configuration edits while a bot is not fully stopped.
	ErrNotStopped = errors.New("bot must be stopped")
	// ErrRunnerUnavailable means lifecycle requests cannot be served because no
	// runner is configured for the node.
	ErrRunnerUnavailable = errors.New("runner unavailable")
)

// ValidationError is a client-correctable input problem; its message is safe to return.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

// Invalid builds a ValidationError.
func Invalid(msg string) error { return &ValidationError{Msg: msg} }

// BusyError means another operation (a deployment, restore or backup) is
// working on the bot. What names it in plain words ("A deployment").
type BusyError struct{ What string }

func (e *BusyError) Error() string {
	return e.What + " is working on this bot right now. Try again when it finishes."
}

// CapacityError refuses work that would exceed a configured budget.
type CapacityError struct {
	Need, Free, Budget int64
}

func (e *CapacityError) Error() string {
	return fmt.Sprintf("not enough reserved memory on this server: the bot needs %s, %s of the %s budget is free; stop another bot or lower this bot's memory",
		mib(e.Need), mib(e.Free), mib(e.Budget))
}

func mib(n int64) string { return fmt.Sprintf("%d MiB", n>>20) }
