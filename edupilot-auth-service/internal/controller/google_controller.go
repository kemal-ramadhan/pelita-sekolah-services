package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/edupilot-auth-service/internal/config"
	"github.com/edupilot-auth-service/internal/service"
	"github.com/edupilot-auth-service/internal/utils"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const oauthStateCookie = "oauth_state"

// googleOauthConfig is built lazily so it sees env loaded at startup.
func googleOauthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("GOOGLE_REDIRECT_URL"),
		Scopes: []string{
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
		},
		Endpoint: google.Endpoint,
	}
}

func setStateCookie(c *gin.Context, value string, maxAge int) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(oauthStateCookie, value, maxAge, "/", os.Getenv("COOKIE_DOMAIN"), config.IsProduction(), true)
}

func frontendURL() string {
	return strings.TrimRight(os.Getenv("FRONTEND_URL"), "/")
}

// googleFail redirects back to the frontend login page with an error code
// (or answers JSON when FRONTEND_URL is not configured).
func googleFail(c *gin.Context, status int, code string, err error) {
	if err != nil && config.Logger != nil {
		config.Logger.Warn("google oauth failed", zap.String("code", code), zap.Error(err))
	}
	if fe := frontendURL(); fe != "" {
		c.Redirect(http.StatusFound, fe+"/login?error="+url.QueryEscape(code))
		return
	}
	c.JSON(status, gin.H{"error": "Login Google gagal", "code": code})
}

// GoogleLogin godoc
// @Summary Redirect user to Google login page
// @Tags Auth
// @Success 307 {string} string "Redirect to Google"
// @Router /auth/google/login [get]
func GoogleLogin(c *gin.Context) {
	cfg := googleOauthConfig()
	if cfg.ClientID == "" || cfg.RedirectURL == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Login Google belum dikonfigurasi", "code": "GOOGLE_DISABLED"})
		return
	}
	state, err := utils.RandomToken(32)
	if err != nil {
		respondError(c, err)
		return
	}
	setStateCookie(c, state, 300)
	c.Redirect(http.StatusTemporaryRedirect, cfg.AuthCodeURL(state))
}

// GoogleCallback godoc
// @Summary Handle Google OAuth2 callback
// @Description On success redirects to FRONTEND_URL/auth/google/callback#access_token=..&refresh_token=..
// @Tags Auth
// @Success 302 {string} string "Redirect to frontend"
// @Router /auth/google/callback [get]
func GoogleCallback(c *gin.Context) {
	cfg := googleOauthConfig()
	state := c.Query("state")
	expectedState, err := c.Cookie(oauthStateCookie)
	setStateCookie(c, "", -1) // state is single-use
	if err != nil || state == "" || !utils.SecureCompare(state, expectedState) {
		googleFail(c, http.StatusBadRequest, "invalid_state", err)
		return
	}

	code := c.Query("code")
	if code == "" {
		googleFail(c, http.StatusBadRequest, "missing_code", nil)
		return
	}

	ctx := c.Request.Context()
	token, err := cfg.Exchange(ctx, code)
	if err != nil {
		googleFail(c, http.StatusBadRequest, "exchange_failed", err)
		return
	}

	resp, err := cfg.Client(ctx, token).Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		googleFail(c, http.StatusBadGateway, "userinfo_failed", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		googleFail(c, http.StatusBadGateway, "userinfo_failed", errors.New(resp.Status))
		return
	}

	var info service.GoogleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		googleFail(c, http.StatusBadGateway, "userinfo_invalid", err)
		return
	}

	login, err := service.GoogleLoginCallback(info)
	if err != nil {
		var appErr *service.AppError
		if errors.As(err, &appErr) {
			googleFail(c, appErr.Status, strings.ToLower(appErr.Code), nil)
			return
		}
		googleFail(c, http.StatusInternalServerError, "internal_error", err)
		return
	}

	fe := frontendURL()
	if fe == "" {
		c.JSON(http.StatusOK, login)
		return
	}
	// Tokens go in the URL fragment so they never reach server/proxy logs.
	frag := url.Values{}
	frag.Set("access_token", login.AccessToken)
	frag.Set("refresh_token", login.RefreshToken)
	c.Redirect(http.StatusFound, fe+"/auth/google/callback#"+frag.Encode())
}
