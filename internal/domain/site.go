package domain

// Site is a static website served by the sites listener. It serves exactly
// one immutable release (CurrentRelease) at a time.
type Site struct {
	ID              string
	WorkspaceID     string
	OwnerID         string
	BotID           *string // set when this is the public page for a Discord bot
	Name            string
	Slug            string
	SPA             bool   // unknown paths serve /index.html (client-side routing)
	CleanURLs       bool   // /about serves about.html or about/index.html
	Mode            string // page | files
	PageTitle       string
	PageDescription string
	PageTheme       string // midnight | daylight | system
	PageAccent      string // validated #rrggbb
	PageHTML        string // trusted author HTML, served only on the sites origin
	PageCSS         string // scoped by origin, never injected into the panel
	WidgetsPublic   bool   // explicit opt-in: declarative bot widgets are public
	CurrentRelease  *string
	Disabled        bool // suspended by an administrator
	RepoFullName    *string
	RepoBranch      *string
	RepoRoot        string
	RepoTokenUser   *string // whose GitHub token deploys the repository
	CreatedAtMS     int64
	UpdatedAtMS     int64

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
	Mode      string
}

// PublicSitePage is the deliberately small, public view used by the separate
// sites listener. It contains no owner identity, console output, environment,
// raw events, command names, or private analytics.
type PublicSitePage struct {
	SiteID, BotID, BotName, DiscordUsername, DiscordAvatarURL string
	Title, Description, Theme, Accent, HTML, CSS              string
	WidgetsPublic                                             bool
	Widgets                                                   []BotWidget
}
