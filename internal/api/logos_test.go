package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(1, 1, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func b64(b []byte) map[string]string {
	return map[string]string{"image": base64.StdEncoding.EncodeToString(b)}
}

// fakeBotToken has the shape of a Discord bot token. It is assembled at run
// time so the source holds nothing that secret scanners mistake for a token.
func fakeBotToken(id, secret string) string {
	return strings.Join([]string{id, "Gabcde", strings.Repeat(secret, 30)}, ".")
}

func TestBotLogoUploadAndDiscordFetch(t *testing.T) {
	e := newEnv(t)
	good, bad := fakeBotToken("MTAwMDAwMDAwMDAwMDAwMDAwMA", "a"), fakeBotToken("MTAwMDAwMDAwMDAwMDAwMDAwMQ", "z")
	var auth string
	discord := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.URL.Path != "/users/@me" || auth != "Bot "+good {
			http.Error(w, "unauthorized", 401)
			return
		}
		w.Write([]byte(`{"id":"1000000000000000000","username":"Red","avatar":"0123456789abcdef0123456789abcdef"}`))
	}))
	t.Cleanup(discord.Close)
	e.bots.DiscordAPI = discord.URL
	c := e.user("a@x.io", "user")
	other := e.user("b@x.io", "user")
	var b botDTO
	json.Unmarshal(c.mustStatus(201, "POST", "/api/v1/bots", map[string]any{"name": "x", "runtime": "python",
		"env": map[string]string{"RED_TOKEN": good}}), &b)
	base := "/api/v1/bots/" + b.ID
	if b.LogoURL != "" || b.CustomLogo {
		t.Fatalf("%+v", b)
	}
	c.mustStatus(404, "GET", base+"/logo", nil)

	// The token is found under Red's variable name and only sent to Discord.
	json.Unmarshal(c.mustStatus(200, "POST", base+"/logo/discord", nil), &b)
	if b.LogoURL != "https://cdn.discordapp.com/avatars/1000000000000000000/0123456789abcdef0123456789abcdef.png?size=256" || b.DiscordUsername != "Red" {
		t.Fatalf("%+v", b)
	}

	// Validation: format, size and encoding.
	var g bytes.Buffer
	gif.Encode(&g, image.NewPaletted(image.Rect(0, 0, 32, 32), []color.Color{color.Black}), nil)
	c.mustStatus(400, "PUT", base+"/logo", b64(g.Bytes()))
	c.mustStatus(400, "PUT", base+"/logo", b64(pngOf(t, 8, 8)))
	c.mustStatus(400, "PUT", base+"/logo", b64(pngOf(t, 600, 600)))
	c.mustStatus(400, "PUT", base+"/logo", map[string]string{"image": "%%%"})
	c.mustStatus(400, "PUT", base+"/logo", b64([]byte("<svg onload=alert(1)>")))
	other.mustStatus(404, "PUT", base+"/logo", b64(pngOf(t, 64, 64)))

	// A custom logo wins over the Discord avatar; removing it falls back.
	logo := pngOf(t, 128, 128)
	json.Unmarshal(c.mustStatus(200, "PUT", base+"/logo", b64(logo)), &b)
	if !b.CustomLogo || !strings.HasPrefix(b.LogoURL, base+"/logo?v=") {
		t.Fatalf("%+v", b)
	}
	resp, body := c.do("GET", base+"/logo", nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" || !bytes.Equal(body, logo) ||
		!strings.Contains(resp.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("%d %v", resp.StatusCode, resp.Header)
	}
	other.mustStatus(404, "GET", base+"/logo", nil)
	json.Unmarshal(c.mustStatus(200, "DELETE", base+"/logo", nil), &b)
	if b.CustomLogo || !strings.HasPrefix(b.LogoURL, "https://cdn.discordapp.com/") {
		t.Fatalf("%+v", b)
	}

	// A wrong token is reported without echoing it.
	c.mustStatus(200, "PUT", base+"/env", map[string]any{"vars": map[string]string{"RED_TOKEN": bad}})
	_, body = c.do("POST", base+"/logo/discord", nil)
	if !strings.Contains(string(body), "rejected the token in RED_TOKEN") || strings.Contains(string(body), "zzzz") {
		t.Fatalf("%s", body)
	}
}

func TestSiteIconOrder(t *testing.T) {
	r := newSiteRig(t)
	c := r.owner
	var site struct {
		ID         string
		IconURL    string `json:"icon_url"`
		CustomLogo bool   `json:"custom_logo"`
	}
	json.Unmarshal(c.mustStatus(201, "POST", "/api/v1/sites", map[string]string{"name": "Docs"}), &site)
	icon := "/api/v1/sites/" + site.ID + "/icon"
	c.mustStatus(404, "GET", icon, nil)

	// The favicon declared by index.html is served from the active release.
	fav := pngOf(t, 32, 32)
	up := zipOf(t, map[string]string{"index.html": `<html><head><link rel="icon" type="image/png" href="/img/fav.png?v=2"></head></html>`,
		"img/fav.png": string(fav), "favicon.ico": "ico"})
	if resp, body := c.raw("POST", "/api/v1/sites/"+site.ID+"/upload", up, "application/zip"); resp.StatusCode != 201 {
		t.Fatalf("upload %d %s", resp.StatusCode, body)
	}
	resp, body := c.do("GET", icon, nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" || !bytes.Equal(body, fav) {
		t.Fatalf("favicon %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	// A custom logo overrides it.
	logo := pngOf(t, 64, 64)
	json.Unmarshal(c.mustStatus(200, "PUT", "/api/v1/sites/"+site.ID+"/logo", b64(logo)), &site)
	if !site.CustomLogo {
		t.Fatalf("%+v", site)
	}
	if _, body := c.do("GET", icon, nil); !bytes.Equal(body, logo) {
		t.Fatal("custom logo not served")
	}
	c.mustStatus(200, "DELETE", "/api/v1/sites/"+site.ID+"/logo", nil)
	if _, body := c.do("GET", icon, nil); !bytes.Equal(body, fav) {
		t.Fatal("favicon not restored")
	}
	stranger := r.e.user("s@x.io", "user")
	stranger.mustStatus(404, "GET", icon, nil)
}
