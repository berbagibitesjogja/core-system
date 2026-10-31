package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/berbagibitesjogja/core-system/internal/auth"
	"github.com/berbagibitesjogja/core-system/internal/config"
	"github.com/berbagibitesjogja/core-system/internal/dbgen"
	"github.com/berbagibitesjogja/core-system/internal/response"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type GoogleService interface {
	AuthCodeURL(state string, opts ...oauth2.AuthCodeOption) string
	Exchange(ctx context.Context, code string, opts ...oauth2.AuthCodeOption) (*oauth2.Token, error)
	FetchUserInfo(ctx context.Context, token *oauth2.Token) (*auth.GoogleUserInfo, error)
}

type realGoogleService struct {
	oauthConfig *oauth2.Config
}

func (s *realGoogleService) AuthCodeURL(state string, opts ...oauth2.AuthCodeOption) string {
	return s.oauthConfig.AuthCodeURL(state, opts...)
}

func (s *realGoogleService) Exchange(ctx context.Context, code string, opts ...oauth2.AuthCodeOption) (*oauth2.Token, error) {
	return s.oauthConfig.Exchange(ctx, code, opts...)
}

func (s *realGoogleService) FetchUserInfo(ctx context.Context, token *oauth2.Token) (*auth.GoogleUserInfo, error) {
	client := s.oauthConfig.Client(ctx, token)
	return auth.FetchGoogleUserInfo(ctx, client, "")
}

type OAuthHandler struct {
	pool          *pgxpool.Pool
	config        *config.Config
	queries       *dbgen.Queries
	googleService GoogleService
}

func NewOAuthHandler(pool *pgxpool.Pool, cfg *config.Config, queries *dbgen.Queries) *OAuthHandler {
	oauthConfig := &oauth2.Config{
		ClientID:     cfg.OAuth.GoogleClientID,
		ClientSecret: cfg.OAuth.GoogleClientSecret,
		RedirectURL:  cfg.OAuth.GoogleRedirectURL,
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     google.Endpoint,
	}

	return &OAuthHandler{
		pool:          pool,
		config:        cfg,
		queries:       queries,
		googleService: &realGoogleService{oauthConfig: oauthConfig},
	}
}

func (h *OAuthHandler) SetGoogleService(service GoogleService) {
	h.googleService = service
}

func (h *OAuthHandler) GoogleLoginHandler(w http.ResponseWriter, r *http.Request) {
	if !h.config.OAuth.IsGoogleConfigured() {
		response.InternalServerError(w, r, errors.New("google oauth credentials not configured"), "Google OAuth is not configured on this server")
		return
	}

	invitationToken := strings.TrimSpace(r.URL.Query().Get("invitation_token"))

	state, nonce, err := auth.GenerateOAuthState(h.config.AppConfig.JWTSecret, invitationToken)
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to generate OAuth state")
		return
	}

	isProd := h.config.AppConfig.Env == "production"
	auth.SetOAuthStateCookie(w, nonce, isProd)

	authURL := h.googleService.AuthCodeURL(state)
	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

func (h *OAuthHandler) GoogleCallbackHandler(w http.ResponseWriter, r *http.Request) {
	isProd := h.config.AppConfig.Env == "production"
	defer auth.ClearOAuthStateCookie(w, isProd)

	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")

	if code == "" || state == "" {
		h.deliverAuthError(w, r, http.StatusBadRequest, "VAL_001", "Missing code or state parameter", errors.New("oauth callback missing code or state"))
		return
	}

	cookie, err := r.Cookie(auth.OAuthStateCookieName)
	if err != nil || cookie.Value == "" {
		h.deliverAuthError(w, r, http.StatusBadRequest, "VAL_001", "Missing or expired OAuth state cookie", errors.New("missing oauth state cookie"))
		return
	}

	invitationToken, err := auth.VerifyOAuthState(h.config.AppConfig.JWTSecret, state, cookie.Value)
	if err != nil {
		h.deliverAuthError(w, r, http.StatusBadRequest, "VAL_001", "Invalid or expired OAuth state", fmt.Errorf("state verification failed: %w", err))
		return
	}

	token, err := h.googleService.Exchange(r.Context(), code)
	if err != nil {
		h.deliverAuthError(w, r, http.StatusBadRequest, "OAUTH_EXCHANGE_FAILED", "Failed to exchange authorization code with Google", fmt.Errorf("google token exchange failed: %w", err))
		return
	}

	userInfo, err := h.googleService.FetchUserInfo(r.Context(), token)
	if err != nil {
		h.deliverAuthError(w, r, http.StatusBadRequest, "OAUTH_USERINFO_FAILED", "Failed to fetch user profile from Google", fmt.Errorf("google userinfo fetch failed: %w", err))
		return
	}

	if !userInfo.EmailVerified {
		h.deliverAuthError(w, r, http.StatusBadRequest, "EMAIL_NOT_VERIFIED", "Your Google email address is not verified", fmt.Errorf("google account email not verified: %s", userInfo.Email))
		return
	}

	userInfo.Email = strings.ToLower(strings.TrimSpace(userInfo.Email))

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		h.deliverAuthError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to start database transaction", err)
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.queries.WithTx(tx)

	linked, err := qtx.GetOAuthAccountByProvider(r.Context(), dbgen.GetOAuthAccountByProviderParams{
		Provider:       "google",
		ProviderUserID: userInfo.Sub,
	})
	if err == nil {
		if !linked.IsActive {
			h.deliverAuthError(w, r, http.StatusUnauthorized, "ACCOUNT_DEACTIVATED", "Your account has been deactivated", fmt.Errorf("deactivated user attempted oauth login: %s", linked.UserID))
			return
		}

		userResp, accessToken, rawRefreshToken, accessExpiry, err := h.issueAuthTokens(r.Context(), qtx, linked.UserID, userInfo.Email, string(linked.Role), linked.DivisionID, linked.Name, linked.PhoneNumber, linked.IsActive, linked.EmailVerifiedAt)
		if err != nil {
			h.deliverAuthError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to issue authentication tokens", err)
			return
		}

		if err := tx.Commit(r.Context()); err != nil {
			h.deliverAuthError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to commit transaction", err)
			return
		}

		h.deliverAuthTokens(w, r, accessToken, rawRefreshToken, accessExpiry, userResp)
		return
	}

	existingUser, err := qtx.GetUserByEmail(r.Context(), userInfo.Email)
	if err == nil {
		if !existingUser.IsActive {
			h.deliverAuthError(w, r, http.StatusUnauthorized, "ACCOUNT_DEACTIVATED", "Your account has been deactivated", fmt.Errorf("deactivated user attempted oauth auto-link: %s", existingUser.ID))
			return
		}

		_, err = qtx.CreateOAuthAccount(r.Context(), dbgen.CreateOAuthAccountParams{
			UserID:         existingUser.ID,
			Provider:       "google",
			ProviderUserID: userInfo.Sub,
			Email:          userInfo.Email,
		})
		if err != nil {
			h.deliverAuthError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to link Google account", err)
			return
		}

		if !existingUser.EmailVerifiedAt.Valid {
			_ = qtx.MarkEmailVerified(r.Context(), existingUser.ID)
			existingUser.EmailVerifiedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
		}

		userResp, accessToken, rawRefreshToken, accessExpiry, err := h.issueAuthTokens(r.Context(), qtx, existingUser.ID, existingUser.Email, string(existingUser.Role), existingUser.DivisionID, existingUser.Name, existingUser.PhoneNumber, existingUser.IsActive, existingUser.EmailVerifiedAt)
		if err != nil {
			h.deliverAuthError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to issue authentication tokens", err)
			return
		}

		if err := tx.Commit(r.Context()); err != nil {
			h.deliverAuthError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to commit transaction", err)
			return
		}

		h.deliverAuthTokens(w, r, accessToken, rawRefreshToken, accessExpiry, userResp)
		return
	}

	var invitation dbgen.UserInvitation
	var inviteFound bool

	if invitationToken != "" {
		tokenHash := auth.HashToken(invitationToken)
		inv, err := qtx.GetInvitationByTokenHash(r.Context(), tokenHash)
		if err == nil && !inv.AcceptedAt.Valid && inv.ExpiresAt.After(time.Now()) {
			invitation = inv
			inviteFound = true
		}
	}

	if !inviteFound {
		inv, err := qtx.GetActiveInvitationByEmail(r.Context(), userInfo.Email)
		if err == nil {
			invitation = inv
			inviteFound = true
		}
	}

	if inviteFound {
		finalName := strings.TrimSpace(invitation.Name.String)
		if finalName == "" {
			finalName = strings.TrimSpace(userInfo.Name)
		}
		if finalName == "" {
			parts := strings.Split(userInfo.Email, "@")
			finalName = parts[0]
		}

		now := time.Now()
		newUser, err := qtx.CreateUser(r.Context(), dbgen.CreateUserParams{
			DivisionID:      invitation.DivisionID,
			Email:           invitation.Email,
			Role:            invitation.Role,
			Name:            finalName,
			IsActive:        true,
			EmailVerifiedAt: pgtype.Timestamptz{Time: now, Valid: true},
		})
		if err != nil {
			h.deliverAuthError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to register user from invitation", err)
			return
		}

		_, err = qtx.CreateOAuthAccount(r.Context(), dbgen.CreateOAuthAccountParams{
			UserID:         newUser.ID,
			Provider:       "google",
			ProviderUserID: userInfo.Sub,
			Email:          userInfo.Email,
		})
		if err != nil {
			h.deliverAuthError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to link Google account during registration", err)
			return
		}

		_, err = qtx.MarkInvitationAccepted(r.Context(), invitation.ID)
		if err != nil {
			h.deliverAuthError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to mark invitation accepted", err)
			return
		}

		userResp, accessToken, rawRefreshToken, accessExpiry, err := h.issueAuthTokens(r.Context(), qtx, newUser.ID, newUser.Email, string(newUser.Role), newUser.DivisionID, newUser.Name, newUser.PhoneNumber, newUser.IsActive, newUser.EmailVerifiedAt)
		if err != nil {
			h.deliverAuthError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to issue authentication tokens", err)
			return
		}

		if err := tx.Commit(r.Context()); err != nil {
			h.deliverAuthError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to commit transaction", err)
			return
		}

		h.deliverAuthTokens(w, r, accessToken, rawRefreshToken, accessExpiry, userResp)
		return
	}

	h.deliverAuthError(w, r, http.StatusForbidden, "UNINVITED_USER", "System is invite-only. No active invitation found for this Google email.", fmt.Errorf("uninvited stranger rejected during google oauth: %s", userInfo.Email))
}

func (h *OAuthHandler) issueAuthTokens(ctx context.Context, qtx *dbgen.Queries, userID uuid.UUID, email, role string, divisionID int32, name string, phone pgtype.Text, isActive bool, emailVerifiedAt pgtype.Timestamptz) (UserResponse, string, string, time.Duration, error) {
	accessExpiry := h.config.AppConfig.JWTAccessExpiry
	accessToken, err := auth.SignAccessToken(userID, email, role, divisionID, h.config.AppConfig.JWTSecret, accessExpiry)
	if err != nil {
		return UserResponse{}, "", "", 0, fmt.Errorf("failed to sign access token: %w", err)
	}

	rawRefreshToken, err := auth.GenerateRefreshToken()
	if err != nil {
		return UserResponse{}, "", "", 0, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	tokenHash := auth.HashToken(rawRefreshToken)
	refreshExpiry := time.Now().Add(h.config.AppConfig.JWTRefreshExpiry)

	_, err = qtx.CreateRefreshToken(ctx, dbgen.CreateRefreshTokenParams{
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: refreshExpiry,
	})
	if err != nil {
		return UserResponse{}, "", "", 0, fmt.Errorf("failed to store refresh token: %w", err)
	}

	var verifiedAtStr *string
	if emailVerifiedAt.Valid {
		formatted := emailVerifiedAt.Time.Format(time.RFC3339)
		verifiedAtStr = &formatted
	}

	userResp := UserResponse{
		ID:              userID.String(),
		Email:           email,
		Role:            role,
		DivisionID:      divisionID,
		Name:            name,
		PhoneNumber:     phone.String,
		IsActive:        isActive,
		EmailVerifiedAt: verifiedAtStr,
	}

	return userResp, accessToken, rawRefreshToken, accessExpiry, nil
}

func (h *OAuthHandler) isSeparateFrontend() bool {
	frontendURL := strings.TrimSpace(h.config.AppConfig.FrontendURL)
	if frontendURL == "" {
		return false
	}

	backendPort := h.config.AppConfig.Port
	if backendPort == "" {
		backendPort = "8080"
	}

	if strings.Contains(frontendURL, "localhost:"+backendPort) || strings.Contains(frontendURL, "127.0.0.1:"+backendPort) {
		return false
	}

	return true
}

func (h *OAuthHandler) deliverAuthTokens(w http.ResponseWriter, r *http.Request, accessToken, refreshToken string, accessExpiry time.Duration, user UserResponse) {
	if r.Header.Get("Accept") == "application/json" || r.URL.Query().Get("format") == "json" || !h.isSeparateFrontend() {
		response.Success(w, http.StatusOK, "Login successful", AuthTokenResponse{
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
			ExpiresIn:    int64(accessExpiry.Seconds()),
			User:         user,
		})
		return
	}

	frontendURL := h.config.AppConfig.FrontendURL
	redirectURL, err := url.Parse(fmt.Sprintf("%s/auth/callback", strings.TrimRight(frontendURL, "/")))
	if err != nil {
		response.InternalServerError(w, r, err, "Failed to resolve frontend redirect URL")
		return
	}

	q := redirectURL.Query()
	q.Set("access_token", accessToken)
	q.Set("refresh_token", refreshToken)
	redirectURL.RawQuery = q.Encode()

	http.Redirect(w, r, redirectURL.String(), http.StatusTemporaryRedirect)
}

func (h *OAuthHandler) deliverAuthError(w http.ResponseWriter, r *http.Request, status int, code, message string, rawErr error) {
	if rawErr != nil {
		response.SetError(r.Context(), rawErr)
	}

	if r.Header.Get("Accept") == "application/json" || r.URL.Query().Get("format") == "json" || !h.isSeparateFrontend() {
		response.ErrorWithRequest(w, r, status, code, message, nil)
		return
	}

	frontendURL := h.config.AppConfig.FrontendURL
	redirectURL, err := url.Parse(fmt.Sprintf("%s/auth/callback", strings.TrimRight(frontendURL, "/")))
	if err != nil {
		response.ErrorWithRequest(w, r, status, code, message, nil)
		return
	}

	q := redirectURL.Query()
	q.Set("error", code)
	q.Set("message", message)
	if refID := response.GetRequestID(r.Context()); refID != "" {
		q.Set("ref_id", refID)
	}
	redirectURL.RawQuery = q.Encode()

	http.Redirect(w, r, redirectURL.String(), http.StatusTemporaryRedirect)
}
