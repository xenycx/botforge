package sqlite

import (
	"context"

	"botpanel/internal/domain"
)

const siteCols = `s.id, s.workspace_id, s.owner_id, s.name, s.slug, s.spa, s.clean_urls, s.current_release, s.disabled,
	s.repo_full_name, s.repo_branch, s.repo_root, s.repo_token_user, s.created_at_ms, s.updated_at_ms,
	COALESCE(u.email, ''), COALESCE(w.name, ''),
	(SELECT count(*) FROM site_domains d WHERE d.site_id = s.id),
	COALESCE((SELECT r.bytes FROM site_releases r WHERE r.id = s.current_release), 0)`

const siteFrom = ` FROM sites s LEFT JOIN users u ON u.id = s.owner_id LEFT JOIN workspaces w ON w.id = s.workspace_id`

func scanSite(row interface{ Scan(...any) error }) (domain.Site, error) {
	var s domain.Site
	var spa, clean, disabled int
	err := row.Scan(&s.ID, &s.WorkspaceID, &s.OwnerID, &s.Name, &s.Slug, &spa, &clean, &s.CurrentRelease, &disabled,
		&s.RepoFullName, &s.RepoBranch, &s.RepoRoot, &s.RepoTokenUser, &s.CreatedAtMS, &s.UpdatedAtMS,
		&s.OwnerEmail, &s.WorkspaceName, &s.Domains, &s.ReleaseBytes)
	s.SPA, s.CleanURLs, s.Disabled = spa == 1, clean == 1, disabled == 1
	return s, mapErr(err)
}

func (db *DB) listSites(ctx context.Context, where string, args ...any) ([]domain.Site, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+siteCols+siteFrom+` `+where+` ORDER BY lower(s.name), s.created_at_ms LIMIT 2000`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Site
	for rows.Next() {
		s, err := scanSite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CreateSite inserts a site; a taken slug returns domain.ErrConflict.
func (db *DB) CreateSite(ctx context.Context, s domain.Site) error {
	_, err := db.ExecContext(ctx, `INSERT INTO sites (id, workspace_id, owner_id, name, slug, spa, clean_urls, created_at_ms, updated_at_ms)
		VALUES (?,?,?,?,?,?,?,?,?)`, s.ID, s.WorkspaceID, s.OwnerID, s.Name, s.Slug, boolInt(s.SPA), boolInt(s.CleanURLs), s.CreatedAtMS, s.UpdatedAtMS)
	return mapErr(err)
}

// GetSite returns one site.
func (db *DB) GetSite(ctx context.Context, id string) (domain.Site, error) {
	return scanSite(db.QueryRowContext(ctx, `SELECT `+siteCols+siteFrom+` WHERE s.id = ?`, id))
}

// ListSitesForUser returns the sites in the workspaces userID belongs to.
func (db *DB) ListSitesForUser(ctx context.Context, userID string) ([]domain.Site, error) {
	return db.listSites(ctx, `WHERE s.workspace_id IN (SELECT workspace_id FROM workspace_members WHERE user_id = ?)`, userID)
}

// ListAllSites returns every site (administrators).
func (db *DB) ListAllSites(ctx context.Context) ([]domain.Site, error) {
	return db.listSites(ctx, ``)
}

// ListWorkspaceSites returns the sites in one workspace.
func (db *DB) ListWorkspaceSites(ctx context.Context, workspaceID string) ([]domain.Site, error) {
	return db.listSites(ctx, `WHERE s.workspace_id = ?`, workspaceID)
}

// CountOwnedSites counts the sites an account created.
func (db *DB) CountOwnedSites(ctx context.Context, userID string) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT count(*) FROM sites WHERE owner_id = ?`, userID).Scan(&n)
	return n, err
}

// UpdateSite stores the mutable settings of a site.
func (db *DB) UpdateSite(ctx context.Context, s domain.Site) error {
	res, err := db.ExecContext(ctx, `UPDATE sites SET name = ?, spa = ?, clean_urls = ?, repo_full_name = ?, repo_branch = ?, repo_root = ?,
		repo_token_user = ?, workspace_id = ?, updated_at_ms = ? WHERE id = ?`,
		s.Name, boolInt(s.SPA), boolInt(s.CleanURLs), s.RepoFullName, s.RepoBranch, s.RepoRoot, s.RepoTokenUser, s.WorkspaceID, s.UpdatedAtMS, s.ID)
	if err != nil {
		return mapErr(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// SetSiteDisabled suspends or restores a site.
func (db *DB) SetSiteDisabled(ctx context.Context, id string, disabled bool, nowMS int64) error {
	res, err := db.ExecContext(ctx, `UPDATE sites SET disabled = ?, updated_at_ms = ? WHERE id = ?`, boolInt(disabled), nowMS, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ActivateRelease switches the release a site serves. The release must
// belong to the site.
func (db *DB) ActivateRelease(ctx context.Context, siteID, releaseID string, nowMS int64) error {
	res, err := db.ExecContext(ctx, `UPDATE sites SET current_release = ?1, updated_at_ms = ?2
		WHERE id = ?3 AND EXISTS (SELECT 1 FROM site_releases WHERE id = ?1 AND site_id = ?3)`, releaseID, nowMS, siteID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// DeleteSite removes a site with its releases and domains (rows only).
func (db *DB) DeleteSite(ctx context.Context, id string) error {
	res, err := db.ExecContext(ctx, `DELETE FROM sites WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// InsertRelease records a new release.
func (db *DB) InsertRelease(ctx context.Context, r domain.SiteRelease) error {
	_, err := db.ExecContext(ctx, `INSERT INTO site_releases (id, site_id, source, source_label, files, bytes, actor_id, created_at_ms)
		VALUES (?,?,?,?,?,?,?,?)`, r.ID, r.SiteID, r.Source, r.SourceLabel, r.Files, r.Bytes, r.ActorID, r.CreatedAtMS)
	return mapErr(err)
}

// ListReleases returns a site's releases, newest first.
func (db *DB) ListReleases(ctx context.Context, siteID string) ([]domain.SiteRelease, error) {
	rows, err := db.QueryContext(ctx, `SELECT r.id, r.site_id, r.source, r.source_label, r.files, r.bytes, r.actor_id, u.email, r.created_at_ms
		FROM site_releases r LEFT JOIN users u ON u.id = r.actor_id WHERE r.site_id = ? ORDER BY r.created_at_ms DESC, r.rowid DESC LIMIT 100`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SiteRelease
	for rows.Next() {
		var r domain.SiteRelease
		if err := rows.Scan(&r.ID, &r.SiteID, &r.Source, &r.SourceLabel, &r.Files, &r.Bytes, &r.ActorID, &r.ActorEmail, &r.CreatedAtMS); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteRelease removes a release row that is not being served.
func (db *DB) DeleteRelease(ctx context.Context, siteID, releaseID string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM site_releases WHERE id = ?1 AND site_id = ?2
		AND NOT EXISTS (SELECT 1 FROM sites WHERE id = ?2 AND current_release = ?1)`, releaseID, siteID)
	return err
}

// ClaimDomain attaches a domain to a site. A domain verified for another
// site is refused (ErrConflict); an unverified claim by another site is
// replaced, so nobody can reserve a domain they cannot prove they control.
func (db *DB) ClaimDomain(ctx context.Context, d domain.SiteDomain) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var site string
	var verified *int64
	err = tx.QueryRowContext(ctx, `SELECT site_id, verified_at_ms FROM site_domains WHERE domain = ?`, d.Domain).Scan(&site, &verified)
	switch err = mapErr(err); {
	case err == nil && site == d.SiteID:
		return domain.Invalid("this site already has that domain")
	case err == nil && verified != nil:
		return domain.ErrConflict
	case err == nil:
		if _, err := tx.ExecContext(ctx, `DELETE FROM site_domains WHERE domain = ?`, d.Domain); err != nil {
			return err
		}
	case err != domain.ErrNotFound:
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO site_domains (domain, site_id, token, created_at_ms) VALUES (?,?,?,?)`,
		d.Domain, d.SiteID, d.Token, d.CreatedAtMS); err != nil {
		return mapErr(err)
	}
	return tx.Commit()
}

const domainCols = `domain, site_id, token, verified_at_ms, last_checked_at_ms, last_error, created_at_ms`

func scanDomain(row interface{ Scan(...any) error }) (domain.SiteDomain, error) {
	var d domain.SiteDomain
	err := row.Scan(&d.Domain, &d.SiteID, &d.Token, &d.VerifiedAtMS, &d.LastCheckedAtMS, &d.LastError, &d.CreatedAtMS)
	return d, mapErr(err)
}

// ListDomains returns a site's domains.
func (db *DB) ListDomains(ctx context.Context, siteID string) ([]domain.SiteDomain, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+domainCols+` FROM site_domains WHERE site_id = ? ORDER BY domain LIMIT 100`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SiteDomain
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListVerifiedDomains returns every verified domain (for periodic re-checks).
func (db *DB) ListVerifiedDomains(ctx context.Context) ([]domain.SiteDomain, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+domainCols+` FROM site_domains WHERE verified_at_ms IS NOT NULL LIMIT 10000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SiteDomain
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetDomain returns one domain of a site.
func (db *DB) GetDomain(ctx context.Context, siteID, name string) (domain.SiteDomain, error) {
	return scanDomain(db.QueryRowContext(ctx, `SELECT `+domainCols+` FROM site_domains WHERE site_id = ? AND domain = ?`, siteID, name))
}

// RecordDomainCheck stores the outcome of a DNS verification. verifiedAt nil
// keeps the current verification state (a failed re-check only reports).
func (db *DB) RecordDomainCheck(ctx context.Context, name string, verifiedAt *int64, errMsg *string, nowMS int64) error {
	_, err := db.ExecContext(ctx, `UPDATE site_domains SET verified_at_ms = COALESCE(?, verified_at_ms), last_error = ?, last_checked_at_ms = ?
		WHERE domain = ?`, verifiedAt, errMsg, nowMS, name)
	return err
}

// DeleteDomain detaches a domain from a site.
func (db *DB) DeleteDomain(ctx context.Context, siteID, name string) error {
	res, err := db.ExecContext(ctx, `DELETE FROM site_domains WHERE site_id = ? AND domain = ?`, siteID, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// SiteRoutes returns what the sites listener serves: slug routes, and
// verified custom domains.
func (db *DB) SiteRoutes(ctx context.Context) (bySlug, byDomain map[string]domain.SiteRoute, err error) {
	bySlug, byDomain = map[string]domain.SiteRoute{}, map[string]domain.SiteRoute{}
	rows, err := db.QueryContext(ctx, `SELECT s.id, s.slug, COALESCE(s.current_release, ''), s.spa, s.clean_urls, s.disabled, d.domain
		FROM sites s LEFT JOIN site_domains d ON d.site_id = s.id AND d.verified_at_ms IS NOT NULL`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r domain.SiteRoute
		var slug string
		var host *string
		var spa, clean, disabled int
		if err := rows.Scan(&r.SiteID, &slug, &r.Release, &spa, &clean, &disabled, &host); err != nil {
			return nil, nil, err
		}
		r.SPA, r.CleanURLs, r.Disabled = spa == 1, clean == 1, disabled == 1
		bySlug[slug] = r
		if host != nil {
			byDomain[*host] = r
		}
	}
	return bySlug, byDomain, rows.Err()
}
