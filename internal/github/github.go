// Package github is a small GitHub REST client for the deployment feature:
// list repositories and branches, resolve a branch to a commit, download a
// tarball, and manage a push webhook. Downloading a tarball (instead of
// running git on the host) means no repository code, hook or filter ever runs
// on the host.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	// FullNameRe validates "owner/repo".
	FullNameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}$`)
	branchRe   = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,255}$`)
	shaRe      = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// ValidBranch reports whether name is a plausible, safe branch name.
func ValidBranch(name string) bool {
	return branchRe.MatchString(name) && !strings.HasPrefix(name, "-") && !strings.HasPrefix(name, "/") &&
		!strings.Contains(name, "..") && !strings.HasSuffix(name, "/") && !strings.HasSuffix(name, ".lock")
}

// ErrInvalid marks a malformed repository, branch or commit name.
var ErrInvalid = errors.New("invalid repository, branch or commit name")

// ErrNotFound means the repository or ref does not exist or is not visible to the token.
var ErrNotFound = errors.New("repository or branch not found (or no access)")

// ErrUnauthorized means the token was rejected.
var ErrUnauthorized = errors.New("GitHub rejected the token; reconnect your GitHub account")

// Client talks to the GitHub API. API is overridable for tests and GitHub Enterprise.
type Client struct {
	API  string
	HTTP *http.Client
}

// Repo is a repository summary.
type Repo struct {
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
}

func (c *Client) base() string {
	if c.API != "" {
		return strings.TrimRight(c.API, "/")
	}
	return "https://api.github.com"
}

func (c *Client) hc() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (c *Client) do(ctx context.Context, method, path, token string, body any) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base()+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "botpanel")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.hc().Do(req)
}

func statusErr(res *http.Response) error {
	switch res.StatusCode {
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusNotFound, http.StatusForbidden:
		return ErrNotFound // GitHub hides private repositories behind 404
	}
	return fmt.Errorf("github: unexpected status %d", res.StatusCode)
}

func (c *Client) getJSON(ctx context.Context, path, token string, out any) error {
	res, err := c.do(ctx, http.MethodGet, path, token, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return statusErr(res)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(out)
}

// Repos lists repositories the token can push to or read, most recently updated first (up to 300).
func (c *Client) Repos(ctx context.Context, token string) ([]Repo, error) {
	var all []Repo
	for page := 1; page <= 3; page++ {
		var pg []Repo
		q := fmt.Sprintf("/user/repos?per_page=100&sort=updated&affiliation=owner,collaborator,organization_member&page=%d", page)
		if err := c.getJSON(ctx, q, token, &pg); err != nil {
			return nil, err
		}
		all = append(all, pg...)
		if len(pg) < 100 {
			break
		}
	}
	return all, nil
}

// GetRepo returns one repository (token may be empty for public repositories).
func (c *Client) GetRepo(ctx context.Context, token, fullName string) (Repo, error) {
	if !FullNameRe.MatchString(fullName) {
		return Repo{}, ErrInvalid
	}
	var r Repo
	err := c.getJSON(ctx, "/repos/"+fullName, token, &r)
	return r, err
}

// Branches lists branch names (up to 300).
func (c *Client) Branches(ctx context.Context, token, fullName string) ([]string, error) {
	if !FullNameRe.MatchString(fullName) {
		return nil, ErrInvalid
	}
	var out []string
	for page := 1; page <= 3; page++ {
		var pg []struct{ Name string }
		if err := c.getJSON(ctx, fmt.Sprintf("/repos/%s/branches?per_page=100&page=%d", fullName, page), token, &pg); err != nil {
			return nil, err
		}
		for _, b := range pg {
			out = append(out, b.Name)
		}
		if len(pg) < 100 {
			break
		}
	}
	return out, nil
}

// BranchSHA resolves a branch to its head commit.
func (c *Client) BranchSHA(ctx context.Context, token, fullName, branch string) (string, error) {
	if !FullNameRe.MatchString(fullName) || !ValidBranch(branch) {
		return "", ErrInvalid
	}
	var o struct{ SHA string }
	if err := c.getJSON(ctx, "/repos/"+fullName+"/commits/"+url.PathEscape(branch), token, &o); err != nil {
		return "", err
	}
	if !shaRe.MatchString(o.SHA) {
		return "", errors.New("github returned an invalid commit id")
	}
	return o.SHA, nil
}

// Tarball opens the gzip tarball of a commit. The API redirects to codeload;
// Go drops the Authorization header on the cross-host redirect.
func (c *Client) Tarball(ctx context.Context, token, fullName, sha string) (io.ReadCloser, error) {
	if !FullNameRe.MatchString(fullName) || !shaRe.MatchString(sha) {
		return nil, ErrInvalid
	}
	hc := *c.hc()
	hc.Timeout = 0 // bounded by ctx; large repositories take longer than an API call
	cc := &Client{API: c.API, HTTP: &hc}
	res, err := cc.do(ctx, http.MethodGet, "/repos/"+fullName+"/tarball/"+sha, token, nil)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		defer res.Body.Close()
		return nil, statusErr(res)
	}
	return res.Body, nil
}

// CreateHook registers a push webhook and returns its id.
func (c *Client) CreateHook(ctx context.Context, token, fullName, hookURL, secret string) (int64, error) {
	if !FullNameRe.MatchString(fullName) {
		return 0, ErrInvalid
	}
	res, err := c.do(ctx, http.MethodPost, "/repos/"+fullName+"/hooks", token, map[string]any{
		"name": "web", "active": true, "events": []string{"push"},
		"config": map[string]string{"url": hookURL, "content_type": "json", "secret": secret, "insecure_ssl": "0"},
	})
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		return 0, statusErr(res)
	}
	var o struct{ ID int64 }
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&o); err != nil {
		return 0, err
	}
	return o.ID, nil
}

// DeleteHook removes a webhook; a missing one is not an error.
func (c *Client) DeleteHook(ctx context.Context, token, fullName string, id int64) error {
	if !FullNameRe.MatchString(fullName) {
		return ErrInvalid
	}
	res, err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/repos/%s/hooks/%d", fullName, id), token, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNoContent || res.StatusCode == http.StatusNotFound {
		return nil
	}
	return statusErr(res)
}

// ValidSHA reports whether s is a full commit id.
func ValidSHA(s string) bool { return shaRe.MatchString(s) }

// Commit is one commit in a comparison.
type Commit struct {
	SHA     string `json:"sha"`
	Message string `json:"message"` // first line only
	Author  string `json:"author"`
}

// ChangedFile is one file in a comparison.
type ChangedFile struct {
	Path   string `json:"path"`
	Status string `json:"status"` // added | modified | removed | renamed | ...
}

// Comparison describes what changed between two commits (bounded).
type Comparison struct {
	HeadSHA      string        `json:"head_sha"`
	AheadBy      int           `json:"ahead_by"`
	BehindBy     int           `json:"behind_by"`
	Commits      []Commit      `json:"commits"`
	Files        []ChangedFile `json:"files"`
	FilesTrimmed bool          `json:"files_trimmed"`
}

// Compare lists commits and changed files from base to head. GitHub itself
// caps the file list (300); the result is further bounded here.
func (c *Client) Compare(ctx context.Context, token, fullName, base, head string) (Comparison, error) {
	if !FullNameRe.MatchString(fullName) || !shaRe.MatchString(base) || !shaRe.MatchString(head) {
		return Comparison{}, ErrInvalid
	}
	var o struct {
		AheadBy  int `json:"ahead_by"`
		BehindBy int `json:"behind_by"`
		Commits  []struct {
			SHA    string `json:"sha"`
			Commit struct {
				Message string `json:"message"`
				Author  struct {
					Name string `json:"name"`
				} `json:"author"`
			} `json:"commit"`
		} `json:"commits"`
		Files []struct {
			Filename string `json:"filename"`
			Status   string `json:"status"`
		} `json:"files"`
	}
	if err := c.getJSON(ctx, "/repos/"+fullName+"/compare/"+base+"..."+head+"?per_page=50", token, &o); err != nil {
		return Comparison{}, err
	}
	out := Comparison{HeadSHA: head, AheadBy: o.AheadBy, BehindBy: o.BehindBy}
	for i := len(o.Commits) - 1; i >= 0 && len(out.Commits) < 20; i-- { // newest first
		cm := o.Commits[i]
		msg, _, _ := strings.Cut(cm.Commit.Message, "\n")
		out.Commits = append(out.Commits, Commit{SHA: cm.SHA, Message: trunc(msg, 200), Author: trunc(cm.Commit.Author.Name, 100)})
	}
	for _, f := range o.Files {
		if len(out.Files) >= 200 {
			out.FilesTrimmed = true
			break
		}
		out.Files = append(out.Files, ChangedFile{Path: trunc(f.Filename, 300), Status: f.Status})
	}
	return out, nil
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
