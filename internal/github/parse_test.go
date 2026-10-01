package github

import "testing"

func TestParseRepoRef(t *testing.T) {
	ok := map[string]RepoRef{
		"owner/name": {FullName: "owner/name"},
		"https://github.com/Cog-Creators/Red-DiscordBot.git": {FullName: "Cog-Creators/Red-DiscordBot"},
		"github.com/a/b/":                             {FullName: "a/b"},
		"git@github.com:a/b.git":                      {FullName: "a/b"},
		"https://www.github.com/a/b/tree/dev/bot/src": {FullName: "a/b", Branch: "dev", Path: "bot/src"},
		"https://github.com/a/b/blob/main/README.md":  {FullName: "a/b", Branch: "main"},
		"https://github.com/a/b?tab=readme#x":         {FullName: "a/b"},
	}
	for in, want := range ok {
		got, err := ParseRepoRef(in)
		if err != nil || got != want {
			t.Errorf("%q: got %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "justname", "https://github.com/a", "a/b c", "a/../b", "https://github.com/a/b/tree/-x"} {
		if _, err := ParseRepoRef(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}
