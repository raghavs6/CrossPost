package handler

import (
	"log"
	"net/http"
	"net/url"
	"strings"
)

const oauthErrorQueryParam = "oauth_error"

// redirectOAuthCallbackError sends the browser back to the dashboard with a
// safe, non-secret error code. The detailed secret-bearing OAuth request data
// stays out of logs and out of the URL.
func redirectOAuthCallbackError(w http.ResponseWriter, r *http.Request, frontendURL, platform, reason string) {
	log.Printf("oauth callback failed: platform=%s reason=%s path=%s", platform, reason, r.URL.Path)
	http.Redirect(w, r, oauthErrorRedirectURL(frontendURL, platform, reason), http.StatusFound)
}

func oauthErrorRedirectURL(frontendURL, platform, reason string) string {
	u, err := url.Parse(frontendURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		base := strings.TrimRight(frontendURL, "/")
		if base == "" {
			base = ""
		}
		separator := "?"
		if strings.Contains(base, "?") {
			separator = "&"
		}
		return base + "/dashboard" + separator + oauthErrorQueryParam + "=" + url.QueryEscape(platform+"_"+reason)
	}

	u.Path = "/dashboard"
	u.RawQuery = url.Values{oauthErrorQueryParam: []string{platform + "_" + reason}}.Encode()
	u.Fragment = ""
	return u.String()
}
