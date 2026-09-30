package domain

// Site is a static website served by the sites listener. It serves exactly
// one immutable release (CurrentRelease) at a time.
type Site struct {
	ID             string
	WorkspaceID    string
	OwnerID        string
	Name           string
	Slug           string
	SPA            bool // unknown paths serve /index.html (client-side routing)
	CleanURLs      bool // /about serves about.html or about/index.html
	CurrentRelease *string
	Disabled       bool // suspended by an administrator
	RepoFullName   *string
	RepoBranch     *string
	RepoRoot       string
	RepoTokenUser  *string // whose GitHub token deploys the repository
	CreatedAtMS    int64
	UpdatedAtMS    int64

	// Filled by listing queries.
	OwnerEmail    string
	WorkspaceName string
	Domains       int
	ReleaseBytes  int64 // size of the current release
}

// SiteRelease is one deployed, immutable set of files.
type SiteRelease struct {
	ID          string
	SiteID      string
	Source      string // upload | github
	SourceLabel *string
	Files       int
	Bytes       int64
	ActorID     *string
	ActorEmail  *string
	CreatedAtMS int64
}

// SiteDomain is a custom host name attached to a site. It is served only
// once VerifiedAtMS is set (DNS TXT ownership proof).
type SiteDomain struct {
	Domain          string
	SiteID          string
	Token           string
	VerifiedAtMS    *int64
	LastCheckedAtMS *int64
	LastError       *string
	CreatedAtMS     int64
}

// SiteRoute is what the sites listener needs to serve one host name.
type SiteRoute struct {
	SiteID    string
	Release   string
	SPA       bool
	CleanURLs bool
	Disabled  bool
}
