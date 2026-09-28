package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/enfec/agentmesh/backend/internal/auth"
	"github.com/enfec/agentmesh/backend/internal/platform/httpx"
	"github.com/enfec/agentmesh/backend/internal/users"
)

const (
	refreshCookie = "am_refresh"
	cookiePath    = "/v1/auth"
	csrfHeader    = "X-Requested-With"
	csrfValue     = "agentmesh"
)

type tokenResponse struct {
	AccessToken  string      `json:"access_token"`
	ExpiresIn    int         `json:"expires_in"`
	User         *users.User `json:"user"`
	RefreshToken string      `json:"refresh_token,omitempty"`
}

func (s *Server) clientInfo(r *http.Request, client string) auth.ClientInfo {
	return auth.ClientInfo{Client: client, UserAgent: r.UserAgent(), IP: httpx.ClientIP(r.Context()), RequestID: httpx.RequestID(r.Context())}
}

func (s *Server) issue(w http.ResponseWriter, r *http.Request, is *auth.Issued) error {
	u, err := s.Users.Get(r.Context(), s.Pool, is.OrgID, is.UserID)
	if err != nil {
		return err
	}
	resp := tokenResponse{AccessToken: is.AccessToken, ExpiresIn: int(time.Until(is.AccessExpiry).Seconds()), User: u}
	if is.Client == auth.ClientMobile {
		resp.RefreshToken = is.RefreshToken
	} else {
		http.SetCookie(w, &http.Cookie{
			Name: refreshCookie, Value: is.RefreshToken, Path: cookiePath, Expires: is.RefreshExp,
			HttpOnly: true, Secure: s.CookieSecure, SameSite: http.SameSiteStrictMode,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
	return nil
}

func (s *Server) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: refreshCookie, Value: "", Path: cookiePath, MaxAge: -1,
		HttpOnly: true, Secure: s.CookieSecure, SameSite: http.SameSiteStrictMode})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Client   string `json:"client"`
	}
	if err := httpx.DecodeJSON(w, r, &in); err != nil {
		return err
	}
	if in.Client == "" {
		in.Client = auth.ClientWeb
	}
	if in.Client != auth.ClientWeb && in.Client != auth.ClientMobile {
		return httpx.Validation{"client": "must be web or mobile"}.Err()
	}
	if in.Email == "" || in.Password == "" || len(in.Password) > auth.MaxPasswordLength || len(in.Email) > 254 {
		return httpx.WithCode(http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
	}
	if ok, wait := s.loginIP.Allow(httpx.ClientIP(r.Context())); !ok {
		return httpx.RateLimited(wait)
	}
	if ok, wait := s.loginEmail.Allow(strings.ToLower(strings.TrimSpace(in.Email))); !ok {
		return httpx.RateLimited(wait)
	}
	is, err := s.Sessions.Login(r.Context(), in.Email, in.Password, s.clientInfo(r, in.Client))
	if errors.Is(err, auth.ErrInvalidCredentials) {
		return httpx.WithCode(http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
	}
	if err != nil {
		return err
	}
	return s.issue(w, r, is)
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request) error {
	if ok, wait := s.refreshIP.Allow(httpx.ClientIP(r.Context())); !ok {
		return httpx.RateLimited(wait)
	}
	var token string
	if c, err := r.Cookie(refreshCookie); err == nil && c.Value != "" {
		if r.Header.Get(csrfHeader) != csrfValue {
			return httpx.Forbidden("missing " + csrfHeader + " header")
		}
		token = c.Value
	} else if r.ContentLength != 0 {
		var in struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := httpx.DecodeJSON(w, r, &in); err != nil {
			return err
		}
		token = in.RefreshToken
	}
	if token == "" {
		return httpx.Unauthorized("no refresh token")
	}
	is, err := s.Sessions.Refresh(r.Context(), token, s.clientInfo(r, ""))
	if errors.Is(err, auth.ErrInvalidRefresh) {
		s.clearCookie(w)
		return httpx.Unauthorized("refresh token is invalid or expired")
	}
	if err != nil {
		return err
	}
	return s.issue(w, r, is)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) error {
	c, cookieErr := r.Cookie(refreshCookie)
	if cookieErr == nil && r.Header.Get(csrfHeader) != csrfValue {
		return httpx.Forbidden("missing " + csrfHeader + " header")
	}
	// Prefer the access token (audited logout); fall back to the cookie.
	if raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		if v, err := s.Sessions.Tokens().Verify(raw); err == nil && v.SessionID != nil {
			if p, err := s.Sessions.LoadPrincipal(r.Context(), v.Subject, *v.SessionID); err == nil {
				if err := s.Sessions.Logout(r.Context(), p, s.clientInfo(r, "")); err != nil {
					return err
				}
			}
		}
	}
	if cookieErr == nil && c.Value != "" {
		if err := s.Sessions.LogoutByRefresh(r.Context(), c.Value); err != nil {
			return err
		}
	}
	s.clearCookie(w)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) error {
	p := auth.FromContext(r.Context())
	u, err := s.Users.Get(r.Context(), s.Pool, p.OrgID, p.UserID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, u)
	return nil
}

func (s *Server) config(w http.ResponseWriter, _ *http.Request) error {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"gateway_url": s.GatewayURL, "version": Version})
	return nil
}
