package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"botpanel/internal/domain"
	"botpanel/internal/schedule"
)

// ScheduleStore is the persistence the scheduler needs.
type ScheduleStore interface {
	InsertSchedule(ctx context.Context, s domain.Schedule) error
	UpdateSchedule(ctx context.Context, s domain.Schedule) error
	GetSchedule(ctx context.Context, botID, id string) (domain.Schedule, error)
	ListSchedules(ctx context.Context, botID string) ([]domain.Schedule, error)
	DueSchedules(ctx context.Context, nowMS int64, limit int) ([]domain.Schedule, error)
	RecordScheduleRun(ctx context.Context, id string, runMS int64, status, msg string, next *int64, disable bool) error
	DeleteSchedule(ctx context.Context, botID, id string) error
}

// Scheduled actions and the permission each one needs at run time.
var scheduleActions = map[string]int{
	"backup":  domain.PermEditFiles,
	"deploy":  domain.PermEditFiles,
	"start":   domain.PermPower,
	"stop":    domain.PermPower,
	"restart": domain.PermPower,
}

const (
	maxSchedulesPerBot = 20
	// A run found later than this after its due time (the panel was down) is
	// recorded as missed instead of being run late: no backlog after downtime.
	scheduleGrace = 5 * time.Minute
	scheduleTick  = 30 * time.Second
	scheduleBatch = 20
)

// Scheduler runs per-bot scheduled actions. One goroutine polls the database
// for due rows; each action is a typed panel operation executed with the
// CURRENT permissions of the user who owns the schedule.
type Scheduler struct {
	Store   ScheduleStore
	Bots    *BotService
	Backups *BackupService // nil: backup schedules are refused
	Deploy  *DeployService // nil: deploy schedules are refused
	Log     *slog.Logger
	Now     func() time.Time

	wake chan struct{}
	once sync.Once
}

func (s *Scheduler) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Scheduler) init() {
	s.once.Do(func() {
		s.wake = make(chan struct{}, 1)
		if s.Log == nil {
			s.Log = slog.Default()
		}
	})
}

// ScheduleInput creates or changes a schedule.
type ScheduleInput struct {
	Action   *string
	Spec     *string
	Timezone *string
	Enabled  *bool
}

// ScheduleView is a schedule with its upcoming run times.
type ScheduleView struct {
	domain.Schedule
	Upcoming []int64
	CanEdit  bool
}

// ParseSchedule validates a cron specification and time zone.
func ParseSchedule(spec, tz string) (schedule.Spec, *time.Location, error) {
	sp, err := schedule.Parse(spec)
	if err != nil {
		return schedule.Spec{}, nil, domain.Invalid(err.Error())
	}
	tz = strings.TrimSpace(tz)
	if tz == "" {
		tz = "UTC"
	}
	if len(tz) > 64 || strings.ContainsAny(tz, " \x00") || tz == "Local" {
		return schedule.Spec{}, nil, domain.Invalid("choose a time zone such as Europe/Berlin or UTC")
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return schedule.Spec{}, nil, domain.Invalid("unknown time zone " + tz)
	}
	if sp.Next(time.Now(), loc).IsZero() {
		return schedule.Spec{}, nil, domain.Invalid("this schedule never runs")
	}
	return sp, loc, nil
}

// Preview returns the next n run times for a specification.
func (s *Scheduler) Preview(spec, tz string, n int) ([]int64, error) {
	sp, loc, err := ParseSchedule(spec, tz)
	if err != nil {
		return nil, err
	}
	return toMS(sp.NextN(s.now(), loc, n)), nil
}

func toMS(ts []time.Time) []int64 {
	out := make([]int64, len(ts))
	for i, t := range ts {
		out[i] = t.UnixMilli()
	}
	return out
}

func (s *Scheduler) actionAvailable(action string) error {
	perm, ok := scheduleActions[action]
	if !ok || perm == 0 {
		return domain.Invalid("choose backup, start, stop, restart or deploy")
	}
	switch {
	case action == "backup" && s.Backups == nil:
		return domain.Invalid("backups are not enabled on this panel")
	case action == "deploy" && s.Deploy == nil:
		return domain.Invalid("GitHub deployments are not enabled on this panel")
	case (action == "start" || action == "stop" || action == "restart") && s.Bots.Notifier == nil:
		return domain.ErrRunnerUnavailable
	}
	return nil
}

// canManage reports whether the actor may change a schedule: its owner while
// they still hold the action's permission, or a full administrator of the bot.
func (s *Scheduler) canManage(ctx context.Context, actor domain.User, b domain.Bot, sc domain.Schedule) bool {
	mask := s.Bots.Permissions(ctx, actor, b)
	if domain.HasPerm(mask, domain.PermFullAdmin) {
		return true
	}
	return sc.OwnerID == actor.ID && domain.HasPerm(mask, scheduleActions[sc.Action])
}

// List returns a bot's schedules; anyone with access to the bot may read them.
func (s *Scheduler) List(ctx context.Context, actor domain.User, botID string) ([]ScheduleView, error) {
	b, err := s.Bots.Authorize(ctx, actor, botID, permAny)
	if err != nil {
		return nil, err
	}
	rows, err := s.Store.ListSchedules(ctx, botID)
	if err != nil {
		return nil, err
	}
	out := make([]ScheduleView, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.view(ctx, actor, b, r))
	}
	return out, nil
}

func (s *Scheduler) view(ctx context.Context, actor domain.User, b domain.Bot, r domain.Schedule) ScheduleView {
	v := ScheduleView{Schedule: r, CanEdit: s.canManage(ctx, actor, b, r)}
	if r.Enabled {
		if sp, loc, err := ParseSchedule(r.Spec, r.Timezone); err == nil {
			v.Upcoming = toMS(sp.NextN(s.now(), loc, 3))
		}
	}
	return v
}

// Create adds a schedule owned by the actor, who must hold the action's
// permission now (and keep holding it for the schedule to run).
func (s *Scheduler) Create(ctx context.Context, actor domain.User, botID string, in ScheduleInput) (ScheduleView, error) {
	if in.Action == nil || in.Spec == nil {
		return ScheduleView{}, domain.Invalid("choose an action and when it runs")
	}
	action := strings.TrimSpace(*in.Action)
	if err := s.actionAvailable(action); err != nil {
		return ScheduleView{}, err
	}
	b, err := s.Bots.Authorize(ctx, actor, botID, scheduleActions[action])
	if err != nil {
		return ScheduleView{}, err
	}
	tz := "UTC"
	if in.Timezone != nil {
		tz = strings.TrimSpace(*in.Timezone)
	}
	sp, loc, err := ParseSchedule(*in.Spec, tz)
	if err != nil {
		return ScheduleView{}, err
	}
	if tz == "" {
		tz = "UTC"
	}
	existing, err := s.Store.ListSchedules(ctx, botID)
	if err != nil {
		return ScheduleView{}, err
	}
	if len(existing) >= maxSchedulesPerBot {
		return ScheduleView{}, domain.Invalid("a bot can have at most 20 schedules")
	}
	now := s.now()
	next := sp.Next(now, loc).UnixMilli()
	sc := domain.Schedule{ID: uuid.NewString(), BotID: botID, OwnerID: actor.ID, OwnerEmail: actor.Email, Action: action,
		Spec: sp.String(), Timezone: tz, Enabled: in.Enabled == nil || *in.Enabled, NextRunMS: &next,
		CreatedAtMS: now.UnixMilli(), UpdatedAtMS: now.UnixMilli()}
	if err := s.Store.InsertSchedule(ctx, sc); err != nil {
		return ScheduleView{}, err
	}
	s.poke()
	return s.view(ctx, actor, b, sc), nil
}

// Update changes a schedule. Changing the action re-checks the permission.
func (s *Scheduler) Update(ctx context.Context, actor domain.User, botID, id string, in ScheduleInput) (ScheduleView, error) {
	b, err := s.Bots.Authorize(ctx, actor, botID, permAny)
	if err != nil {
		return ScheduleView{}, err
	}
	sc, err := s.Store.GetSchedule(ctx, botID, id)
	if err != nil {
		return ScheduleView{}, err
	}
	if !s.canManage(ctx, actor, b, sc) {
		return ScheduleView{}, domain.ErrForbidden
	}
	if in.Action != nil {
		a := strings.TrimSpace(*in.Action)
		if err := s.actionAvailable(a); err != nil {
			return ScheduleView{}, err
		}
		if _, err := s.Bots.Authorize(ctx, actor, botID, scheduleActions[a]); err != nil {
			return ScheduleView{}, err
		}
		sc.Action = a
	}
	if in.Spec != nil {
		sc.Spec = *in.Spec
	}
	if in.Timezone != nil {
		sc.Timezone = strings.TrimSpace(*in.Timezone)
	}
	if in.Enabled != nil {
		sc.Enabled = *in.Enabled
	}
	sp, loc, err := ParseSchedule(sc.Spec, sc.Timezone)
	if err != nil {
		return ScheduleView{}, err
	}
	if sc.Timezone == "" {
		sc.Timezone = "UTC"
	}
	sc.Spec = sp.String()
	now := s.now()
	next := sp.Next(now, loc).UnixMilli()
	sc.NextRunMS = &next
	sc.UpdatedAtMS = now.UnixMilli()
	if err := s.Store.UpdateSchedule(ctx, sc); err != nil {
		return ScheduleView{}, err
	}
	s.poke()
	return s.view(ctx, actor, b, sc), nil
}

// Delete removes a schedule.
func (s *Scheduler) Delete(ctx context.Context, actor domain.User, botID, id string) error {
	b, err := s.Bots.Authorize(ctx, actor, botID, permAny)
	if err != nil {
		return err
	}
	sc, err := s.Store.GetSchedule(ctx, botID, id)
	if err != nil {
		return err
	}
	if !s.canManage(ctx, actor, b, sc) {
		return domain.ErrForbidden
	}
	return s.Store.DeleteSchedule(ctx, botID, id)
}

// RunNow runs a schedule's action immediately (for testing a schedule). It
// does not change the next due time.
func (s *Scheduler) RunNow(ctx context.Context, actor domain.User, botID, id string) (string, string, error) {
	b, err := s.Bots.Authorize(ctx, actor, botID, permAny)
	if err != nil {
		return "", "", err
	}
	sc, err := s.Store.GetSchedule(ctx, botID, id)
	if err != nil {
		return "", "", err
	}
	if !s.canManage(ctx, actor, b, sc) {
		return "", "", domain.ErrForbidden
	}
	status, msg := s.execute(ctx, sc)
	var next *int64
	if sc.NextRunMS != nil {
		n := *sc.NextRunMS
		next = &n
	}
	fctx, cancel := bg(ctx)
	defer cancel()
	_ = s.Store.RecordScheduleRun(fctx, sc.ID, s.now().UnixMilli(), status, msg, next, status == "denied")
	return status, msg, nil
}

func (s *Scheduler) poke() {
	s.init()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Run polls for due schedules until ctx ends.
func (s *Scheduler) Run(ctx context.Context) {
	s.init()
	t := time.NewTicker(scheduleTick)
	defer t.Stop()
	for {
		s.RunDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.wake:
		}
	}
}

// RunDue executes every schedule that is due now, one after another. It is
// exported for tests.
func (s *Scheduler) RunDue(ctx context.Context) {
	s.init()
	now := s.now()
	due, err := s.Store.DueSchedules(ctx, now.UnixMilli(), scheduleBatch)
	if err != nil {
		if ctx.Err() == nil {
			s.Log.Warn("scheduled actions: list due", "err", err)
		}
		return
	}
	for _, sc := range due {
		if ctx.Err() != nil {
			return
		}
		s.runOne(ctx, sc, now)
	}
}

func (s *Scheduler) runOne(ctx context.Context, sc domain.Schedule, now time.Time) {
	var next *int64
	disable := false
	sp, loc, perr := ParseSchedule(sc.Spec, sc.Timezone)
	if perr == nil {
		if n := sp.Next(now, loc); !n.IsZero() {
			ms := n.UnixMilli()
			next = &ms
		}
	}
	if next == nil {
		disable = true
	}
	var status, msg string
	switch {
	case perr != nil:
		status, msg = "failed", "the schedule is no longer valid: "+perr.Error()
	case sc.NextRunMS != nil && now.Sub(time.UnixMilli(*sc.NextRunMS)) > scheduleGrace:
		// The panel was not running at the due time: record it, do not catch up.
		status, msg = "missed", "the panel was not running at "+time.UnixMilli(*sc.NextRunMS).In(loc).Format("2006-01-02 15:04 MST")
	default:
		status, msg = s.execute(ctx, sc)
		if status == "denied" {
			disable = true
		}
	}
	fctx, cancel := bg(ctx)
	defer cancel()
	if err := s.Store.RecordScheduleRun(fctx, sc.ID, now.UnixMilli(), status, msg, next, disable); err != nil {
		s.Log.Warn("scheduled actions: record run", "schedule", sc.ID, "err", err)
	}
}

// execute performs the action as the schedule's owner with their current
// permissions. A disabled account or removed permission is "denied".
func (s *Scheduler) execute(ctx context.Context, sc domain.Schedule) (status, msg string) {
	owner, err := s.Bots.Store.GetUserByID(ctx, sc.OwnerID)
	if err != nil || owner.Disabled {
		return "denied", "the schedule's owner can no longer sign in, so the schedule was paused"
	}
	perm := scheduleActions[sc.Action]
	b, err := s.Bots.Authorize(ctx, owner, sc.BotID, perm)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrForbidden) {
			return "denied", owner.Email + " no longer has permission for this action, so the schedule was paused"
		}
		return "failed", "the bot could not be loaded"
	}
	if err := s.actionAvailable(sc.Action); err != nil {
		return "failed", runMessage(err)
	}
	switch sc.Action {
	case "backup":
		_, err = s.Backups.CreateScheduled(ctx, owner, sc.BotID)
		if err == nil {
			return "ok", "backup started"
		}
	case "deploy":
		err = s.Deploy.DeployAs(ctx, owner, sc.BotID, "", "schedule")
		if err == nil {
			return "ok", "deployment started"
		}
	case "start":
		_, err = s.Bots.Start(ctx, owner, sc.BotID)
		if err == nil {
			return "ok", "start requested"
		}
	case "stop":
		_, err = s.Bots.Stop(ctx, owner, sc.BotID)
		if err == nil {
			return "ok", "stop requested"
		}
	case "restart":
		// A timetable restart never starts a bot someone stopped.
		if b.DesiredState != domain.DesiredRunning {
			return "skipped", "the bot is stopped"
		}
		_, err = s.Bots.Restart(ctx, owner, sc.BotID)
		if err == nil {
			return "ok", "restart requested"
		}
	}
	if errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrNotStopped) {
		return "skipped", runMessage(err)
	}
	return "failed", runMessage(err)
}

func runMessage(err error) string {
	var v *domain.ValidationError
	switch {
	case errors.As(err, &v):
		return trimMsg(v.Msg)
	case errors.As(err, new(*domain.CapacityError)):
		return trimMsg(err.Error())
	case errors.Is(err, domain.ErrConflict):
		return trimMsg(err.Error())
	case errors.Is(err, domain.ErrRunnerUnavailable):
		return "the panel has no Docker runner"
	case errors.Is(err, domain.ErrNotFound):
		return "not found (is a GitHub repository linked?)"
	}
	return "the action could not be started"
}

func trimMsg(s string) string {
	if r := []rune(s); len(r) > 300 {
		return string(r[:297]) + "..."
	}
	return s
}
