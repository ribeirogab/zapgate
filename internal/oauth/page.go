package oauth

import (
	"html/template"
	"net/http"
	"strconv"
	"time"
)

const (
	contentSecurityPolicy = "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'"
	pageStyle             = `
:root { color-scheme: light dark; --bg: #f6f7f9; --card: #ffffff; --text: #1c1e21; --muted: #5f6368; --line: #d6d9de; --accent: #1f7a4d; --accent-text: #ffffff; --warn-bg: #fff4e5; --warn-text: #7a4a00; --error-bg: #fdecea; --error-text: #8a1c14; }
@media (prefers-color-scheme: dark) { :root { --bg: #121417; --card: #1c1f24; --text: #e8eaed; --muted: #a0a6ad; --line: #3a3f46; --accent: #34a46c; --accent-text: #0b1a12; --warn-bg: #3a2c12; --warn-text: #ffd699; --error-bg: #3d1915; --error-text: #ffb4ab; } }
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--text); font: 16px/1.5 system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; }
main { max-width: 28rem; margin: 0 auto; padding: 24px 16px 48px; }
.card { background: var(--card); border: 1px solid var(--line); border-radius: 12px; padding: 20px; }
h1 { font-size: 1.35rem; margin: 0 0 8px; }
p { margin: 0 0 12px; }
.muted { color: var(--muted); font-size: 0.95rem; }
.notice { border-radius: 8px; padding: 10px 12px; margin: 0 0 16px; font-size: 0.95rem; }
.warn { background: var(--warn-bg); color: var(--warn-text); }
.error { background: var(--error-bg); color: var(--error-text); }
label { display: block; font-weight: 600; margin: 16px 0 6px; }
input[type=password], input[type=text] { width: 100%; font-size: 16px; padding: 12px; border: 1px solid var(--line); border-radius: 8px; background: var(--bg); color: var(--text); }
fieldset { border: 0; margin: 16px 0 0; padding: 0; }
legend { font-weight: 600; margin-bottom: 6px; padding: 0; }
.account { display: flex; align-items: center; gap: 12px; padding: 12px; border: 1px solid var(--line); border-radius: 8px; margin-bottom: 8px; font-weight: 400; }
.account input { width: 22px; height: 22px; margin: 0; flex: none; }
.account small { display: block; color: var(--muted); }
.actions { display: flex; gap: 12px; margin-top: 24px; }
button { flex: 1; font-size: 16px; font-weight: 600; padding: 14px; border-radius: 8px; border: 1px solid var(--line); background: var(--card); color: var(--text); }
button.primary { background: var(--accent); border-color: var(--accent); color: var(--accent-text); }
code { font-size: 0.9em; word-break: break-all; }
`
)

var pageTemplates = template.Must(template.New("authorize").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>Authorize access to WhatsApp</title>
<style>{{.Style}}</style>
</head>
<body>
<main>
<div class="card">
<h1>Authorize access to WhatsApp</h1>
<p><strong>{{.ClientName}}</strong> asks for access to your WhatsApp accounts.</p>
<p class="muted">After you authorize, the browser goes back to <code>{{.RedirectHost}}</code>.</p>
{{if .Loopback}}<p class="notice warn">This address is local. Authorize only if you started the connection on this computer yourself.</p>{{end}}
{{if .Error}}<p class="notice error" role="alert">{{.Error}}</p>{{end}}
<form method="post" action="/oauth/authorize">
{{range .Hidden}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">
{{end}}
<label for="password">Owner password</label>
<input type="password" id="password" name="password" value="" autocomplete="current-password" autofocus>
<label for="access_name">Access name</label>
<input type="text" id="access_name" name="access_name" value="{{.AccessName}}" autocomplete="off" autocapitalize="none" spellcheck="false" maxlength="64">
<p class="muted">Lowercase letters, digits and hyphens. The name shows in <code>zapgate access list</code>.</p>
<fieldset>
<legend>Accounts</legend>
{{range .Accounts}}<label class="account"><input type="checkbox" name="accounts" value="{{.Name}}"{{if .Checked}} checked{{end}}><span>{{.Name}}<small>{{.Detail}}</small></span></label>
{{else}}<p class="muted">No account is linked yet. Later, give accounts to this access with <code>zapgate access set</code>.</p>
{{end}}
</fieldset>
<div class="actions">
<button type="submit" name="decision" value="deny">Deny</button>
<button type="submit" name="decision" value="allow" class="primary">Authorize</button>
</div>
</form>
</div>
</main>
</body>
</html>
`))

var errorTemplate = template.Must(template.New("error").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>{{.Style}}</style>
</head>
<body>
<main>
<div class="card">
<h1>{{.Title}}</h1>
<p class="notice error" role="alert">{{.Message}}</p>
<p class="muted">Go back to the app and start the connection again.</p>
</div>
</main>
</body>
</html>
`))

type hiddenField struct {
	Name  string
	Value string
}

type accountChoice struct {
	Name    string
	Detail  string
	Checked bool
}

type pageData struct {
	Style        template.CSS
	ClientName   string
	RedirectHost string
	Loopback     bool
	Error        string
	Hidden       []hiddenField
	AccessName   string
	Accounts     []accountChoice
}

type errorPage struct {
	Style   template.CSS
	Title   string
	Message string
}

func writePageHeaders(w http.ResponseWriter, status int) {
	h := w.Header()
	h.Set(headerContentType, contentTypeHTML)
	h.Set(headerCacheControl, cacheControlNoStore)
	h.Set(headerPragma, pragmaNoCache)
	h.Set("X-Frame-Options", "DENY")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", contentSecurityPolicy)
	w.WriteHeader(status)
}

func renderAuthorizePage(w http.ResponseWriter, status int, data pageData) {
	data.Style = template.CSS(pageStyle)
	writePageHeaders(w, status)
	pageTemplates.Execute(w, data)
}

func renderErrorPage(w http.ResponseWriter, status int, title, message string) {
	writePageHeaders(w, status)
	errorTemplate.Execute(w, errorPage{Style: template.CSS(pageStyle), Title: title, Message: message})
}

func renderBlockedPage(w http.ResponseWriter, retryAfter time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
	renderErrorPage(w, http.StatusTooManyRequests, "Attempts blocked", "Too many wrong passwords from this address. Try again in 15 minutes.")
}
