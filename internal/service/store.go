// Package service holds application rules; it depends on small interfaces so
// it never touches Fiber or Docker.
package service

import (
	"context"

	"botpanel/internal/domain"
)

// Store is the persistence surface the services need.
type Store interface {
	CreateUser(ctx context.Context, u domain.User) error
	GetUserByEmail(ctx context.Context, email string) (domain.User, error)
	GetUserByID(ctx context.Context, id string) (domain.User, error)
	ListUsers(ctx context.Context) ([]domain.User, error)
	SetUserDisabled(ctx context.Context, id string, disabled bool, nowMS int64) error
	SetUserRole(ctx context.Context, id, role string, nowMS int64) error
	UpdateProfile(ctx context.Context, id, name string, avatar []byte, replaceAvatar bool, nowMS int64) error
	BotCountsByOwner(ctx context.Context) (map[string]int, error)

	CreateSession(ctx context.Context, tokenHash []byte, s domain.Session) error
	GetSession(ctx context.Context, tokenHash []byte, nowMS int64) (domain.User, domain.Session, error)
	DeleteSession(ctx context.Context, tokenHash []byte) error
	TouchSession(ctx context.Context, tokenHash []byte, nowMS int64) error
	ListSessions(ctx context.Context, userID string, nowMS int64) ([]domain.Session, error)
	DeleteSessionByID(ctx context.Context, userID, id string) error
	DeleteOtherSessions(ctx context.Context, userID string, keep []byte) (int64, error)
	SetPassword(ctx context.Context, userID, hash string, keep []byte, nowMS int64) error
	MarkSessionAuthenticated(ctx context.Context, tokenHash []byte, nowMS int64) error
	MFAEnabled(ctx context.Context, userID string) (bool, error)
	InsertAccountInvite(ctx context.Context, v domain.AccountInvite, hash []byte) error
	ListAccountInvites(ctx context.Context, nowMS int64) ([]domain.AccountInvite, error)
	AccountInviteByHash(ctx context.Context, hash []byte, nowMS int64) (domain.AccountInvite, error)
	UseAccountInvite(ctx context.Context, hash []byte, u domain.User, nowMS int64) error
	DeleteAccountInvite(ctx context.Context, id string) error

	InsertAPIKey(ctx context.Context, k domain.APIKey) error
	GetUserByAPIKey(ctx context.Context, hash []byte, scope string, nowMS int64) (domain.User, error)
	ListAPIKeys(ctx context.Context, userID string) ([]domain.APIKey, error)
	APIKeyValid(ctx context.Context, hash []byte, userID, scope string, nowMS int64) (bool, error)
	DeleteAPIKey(ctx context.Context, userID, id string) error

	InsertBackup(ctx context.Context, b domain.Backup) error
	FinishBackup(ctx context.Context, id, status string, size int64, sha, errMsg *string) error
	GetBackup(ctx context.Context, botID, id string) (domain.Backup, error)
	ListBackups(ctx context.Context, botID string) ([]domain.Backup, error)
	ListBackupsByKind(ctx context.Context, botID, kind string) ([]domain.Backup, error)
	DeleteBackup(ctx context.Context, botID, id string) error
	SetBackupLabel(ctx context.Context, botID, id string, label *string) error
	SetBackupVerified(ctx context.Context, id string, atMS int64, verr *string) error
	LastBackupAtMS(ctx context.Context, botID, kind string) (int64, error)
	FailStaleBackups(ctx context.Context) (int64, error)
	BotIDs(ctx context.Context) (map[string]bool, error)
	ReplaceEnvAll(ctx context.Context, botID string, vars []domain.EnvVar, nowMS int64) error
	UpsertGitHubRepo(ctx context.Context, r domain.GitHubRepo) error
	GetGitHubRepo(ctx context.Context, botID string) (domain.GitHubRepo, error)
	DeleteGitHubRepo(ctx context.Context, botID string) error
	ListAutoDeployRepos(ctx context.Context, fullName string) ([]domain.GitHubRepo, error)
	RecordDeploy(ctx context.Context, botID string, sha *string, errMsg *string, nowMS int64) error
	SetBotPorts(ctx context.Context, botID string, gen int64, ports []domain.BotPort, nowMS int64) error
	SetBotTelemetryKey(ctx context.Context, botID string, hash []byte, nowMS int64) error

	GetNode(ctx context.Context, id string) (domain.Node, error)

	SetBotTags(ctx context.Context, botID string, tags []string) error
	TagsAndFavorites(ctx context.Context, userID string) (map[string][]string, map[string]bool, error)
	SetFavorite(ctx context.Context, userID, botID string, on bool, nowMS int64) error

	CreateBot(ctx context.Context, b domain.Bot) error
	GetBot(ctx context.Context, id string) (domain.Bot, error)
	ListBots(ctx context.Context, ownerID string) ([]domain.Bot, error)
	ListBotsForUser(ctx context.Context, userID string) ([]domain.Bot, error)
	GetSubUserPermissions(ctx context.Context, botID, userID string) (int, error)
	SetSubUser(ctx context.Context, s domain.SubUser) error
	RemoveSubUser(ctx context.Context, botID, userID string) error
	ListSubUsers(ctx context.Context, botID string) ([]domain.SubUser, error)
	TransferBot(ctx context.Context, botID, newOwner string, keepPrevious bool, nowMS int64) (bool, error)
	InsertInvite(ctx context.Context, v domain.Invite, hash []byte, nowMS int64) error
	ListInvites(ctx context.Context, botID string, nowMS int64) ([]domain.Invite, error)
	InviteByHash(ctx context.Context, hash []byte, nowMS int64) (domain.Invite, error)
	AcceptInvite(ctx context.Context, hash []byte, userID string, nowMS int64) (domain.Invite, error)
	DeleteInvite(ctx context.Context, botID, id string) error
	UpdateBotConfig(ctx context.Context, b domain.Bot, nowMS int64) error
	SetDesired(ctx context.Context, id, desired string, force bool, nowMS int64) (domain.Bot, bool, error)
	SetDesiredWithin(ctx context.Context, id, desired string, force bool, nowMS, nodeMemory int64) (domain.Bot, bool, error)
	OwnerUsage(ctx context.Context, ownerID string) (int, int64, error)
	NodeReserved(ctx context.Context, nodeID string) (int, int64, error)
	RestartIfRunning(ctx context.Context, id string, nowMS int64) (domain.Bot, bool, error)
	MarkBotDeleted(ctx context.Context, id string, nowMS int64) error
	DeleteBotRow(ctx context.Context, id string) error

	ListEnv(ctx context.Context, botID string) ([]domain.EnvVar, error)
	UpsertEnv(ctx context.Context, botID string, vars []domain.EnvVar, nowMS int64) error
	DeleteEnv(ctx context.Context, botID, name string, nowMS int64) error
}
