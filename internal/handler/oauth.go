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

type oauthInvitationInfo struct {
	ID           uuid.UUID
	Role         dbgen.UserRole
	DivisionID   int32
	DivisionName string
	Name         pgtype.Text
	PhoneNumber  pgtype.Text
}

type OAuthUserClaims struct {
	UserID          uuid.UUID
	Email           string
	Name            string
	PhoneNumber     pgtype.Text
	IsActive        bool
	IsStaff         bool
	Role            string
	DivisionID      int32
	DivisionName    string
	IsHero          bool
	EmailVerifiedAt pgtype.Timestamptz
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

	var verifiedInvitation *oauthInvitationInfo
	if invitationToken != "" {
		tokenHash := auth.HashToken(invitationToken)
		inv, err := qtx.GetInvitationByTokenHash(r.Context(), tokenHash)
		if err != nil || inv.AcceptedAt.Valid || !inv.ExpiresAt.After(time.Now()) {
			h.deliverAuthError(w, r, http.StatusBadRequest, "INVITATION_INVALID", "Invitation token is invalid or expired", fmt.Errorf("invalid or expired invitation token"))
			return
		}
		if !strings.EqualFold(strings.TrimSpace(userInfo.Email), strings.TrimSpace(inv.Email)) {
			h.deliverAuthError(w, r, http.StatusForbidden, "INVITATION_EMAIL_MISMATCH", "This invitation was issued to a different email address", fmt.Errorf("oauth email %s does not match invitation email %s", userInfo.Email, inv.Email))
			return
		}
		verifiedInvitation = &oauthInvitationInfo{
			ID:           inv.ID,
			Role:         inv.Role,
			DivisionID:   inv.DivisionID,
			DivisionName: inv.DivisionName,
			Name:         inv.Name,
			PhoneNumber:  inv.PhoneNumber,
		}
	}

	linked, err := qtx.GetOAuthAccountByProvider(r.Context(), dbgen.GetOAuthAccountByProviderParams{
		Provider:       "google",
		ProviderUserID: userInfo.Sub,
	})
	if err == nil {
		if !linked.IsActive {
			h.deliverAuthError(w, r, http.StatusUnauthorized, "ACCOUNT_DEACTIVATED", "Your account has been deactivated", fmt.Errorf("deactivated user attempted oauth login: %s", linked.UserID))
			return
		}

		var staffRole string
		if linked.StaffRole.Valid {
			staffRole = string(linked.StaffRole.UserRole)
		}
		var staffDivisionID int32
		var staffDivisionName string
		if linked.StaffDivisionID.Valid {
			staffDivisionID = linked.StaffDivisionID.Int32
		}
		if linked.StaffDivisionName.Valid {
			staffDivisionName = linked.StaffDivisionName.String
		}

		if !linked.IsStaff {
			var activeInv *oauthInvitationInfo
			if verifiedInvitation != nil {
				activeInv = verifiedInvitation
			} else if inv, invErr := qtx.GetActiveInvitationByEmail(r.Context(), userInfo.Email); invErr == nil {
				activeInv = &oauthInvitationInfo{
					ID:           inv.ID,
					Role:         inv.Role,
					DivisionID:   inv.DivisionID,
					DivisionName: inv.DivisionName,
					Name:         inv.Name,
					PhoneNumber:  inv.PhoneNumber,
				}
			}
			if activeInv != nil {
				_, err = qtx.CreateStaff(r.Context(), dbgen.CreateStaffParams{
					UserID:     linked.UserID,
					DivisionID: activeInv.DivisionID,
					Role:       activeInv.Role,
				})
				if err == nil {
					linked.IsStaff = true
					linked.StaffRole = dbgen.NullUserRole{UserRole: activeInv.Role, Valid: true}
					linked.StaffDivisionID = pgtype.Int4{Int32: activeInv.DivisionID, Valid: true}
					staffRole = string(activeInv.Role)
					staffDivisionID = activeInv.DivisionID
					staffDivisionName = activeInv.DivisionName
					_, _ = qtx.MarkInvitationAccepted(r.Context(), activeInv.ID)
				}
			}
		}

		userResp, accessToken, rawRefreshToken, accessExpiry, err := h.issueAuthTokens(
			r.Context(), qtx, OAuthUserClaims{
				UserID:          linked.UserID,
				Email:           userInfo.Email,
				Name:            linked.Name,
				PhoneNumber:     linked.PhoneNumber,
				IsActive:        linked.IsActive,
				IsStaff:         linked.IsStaff,
				Role:            staffRole,
				DivisionID:      staffDivisionID,
				DivisionName:    staffDivisionName,
				IsHero:          linked.IsHero,
				EmailVerifiedAt: linked.EmailVerifiedAt,
			},
		)
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

	existingUser, err := qtx.GetUserWithRolesByEmail(r.Context(), userInfo.Email)
	if err == nil {
		if !existingUser.IsActive {
			h.deliverAuthError(w, r, http.StatusUnauthorized, "ACCOUNT_DEACTIVATED", "Your account has been deactivated", fmt.Errorf("deactivated user attempted oauth login: %s", existingUser.ID))
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
			now := time.Now()
			_ = qtx.MarkEmailVerified(r.Context(), existingUser.ID)
			_ = qtx.ClearUserPassword(r.Context(), existingUser.ID)
			_ = qtx.RevokeAllUserRefreshTokens(r.Context(), existingUser.ID)
			existingUser.EmailVerifiedAt = pgtype.Timestamptz{Time: now, Valid: true}
		}

		var staffRole string
		if existingUser.StaffRole.Valid {
			staffRole = string(existingUser.StaffRole.UserRole)
		}
		var staffDivisionID int32
		var staffDivisionName string
		if existingUser.StaffDivisionID.Valid {
			staffDivisionID = existingUser.StaffDivisionID.Int32
		}
		if existingUser.StaffDivisionName.Valid {
			staffDivisionName = existingUser.StaffDivisionName.String
		}

		if !existingUser.IsStaff {
			var activeInv *oauthInvitationInfo
			if verifiedInvitation != nil {
				activeInv = verifiedInvitation
			} else if inv, invErr := qtx.GetActiveInvitationByEmail(r.Context(), userInfo.Email); invErr == nil {
				activeInv = &oauthInvitationInfo{
					ID:           inv.ID,
					Role:         inv.Role,
					DivisionID:   inv.DivisionID,
					DivisionName: inv.DivisionName,
					Name:         inv.Name,
					PhoneNumber:  inv.PhoneNumber,
				}
			}
			if activeInv != nil {
				_, err = qtx.CreateStaff(r.Context(), dbgen.CreateStaffParams{
					UserID:     existingUser.ID,
					DivisionID: activeInv.DivisionID,
					Role:       activeInv.Role,
				})
				if err == nil {
					existingUser.IsStaff = true
					existingUser.StaffRole = dbgen.NullUserRole{UserRole: activeInv.Role, Valid: true}
					existingUser.StaffDivisionID = pgtype.Int4{Int32: activeInv.DivisionID, Valid: true}
					staffRole = string(activeInv.Role)
					staffDivisionID = activeInv.DivisionID
					staffDivisionName = activeInv.DivisionName
				}
				_, _ = qtx.MarkInvitationAccepted(r.Context(), activeInv.ID)
			}
		}

		userResp, accessToken, rawRefreshToken, accessExpiry, err := h.issueAuthTokens(
			r.Context(), qtx, OAuthUserClaims{
				UserID:          existingUser.ID,
				Email:           existingUser.Email,
				Name:            existingUser.Name,
				PhoneNumber:     existingUser.PhoneNumber,
				IsActive:        existingUser.IsActive,
				IsStaff:         existingUser.IsStaff,
				Role:            staffRole,
				DivisionID:      staffDivisionID,
				DivisionName:    staffDivisionName,
				IsHero:          existingUser.IsHero,
				EmailVerifiedAt: existingUser.EmailVerifiedAt,
			},
		)
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

	var invitation *oauthInvitationInfo
	if verifiedInvitation != nil {
		invitation = verifiedInvitation
	} else if inv, invErr := qtx.GetActiveInvitationByEmail(r.Context(), userInfo.Email); invErr == nil {
		invitation = &oauthInvitationInfo{
			ID:           inv.ID,
			Role:         inv.Role,
			DivisionID:   inv.DivisionID,
			DivisionName: inv.DivisionName,
			Name:         inv.Name,
			PhoneNumber:  inv.PhoneNumber,
		}
	}

	if invitation != nil {
		staffName := strings.TrimSpace(userInfo.Name)
		if invitation.Name.Valid && invitation.Name.String != "" {
			staffName = invitation.Name.String
		}
		if staffName == "" {
			staffName = userInfo.Email
		}

		var pfpParam pgtype.Text
		if userInfo.Picture != "" {
			pfpParam = pgtype.Text{String: userInfo.Picture, Valid: true}
		}

		now := time.Now()
		newUser, err := qtx.CreateUser(r.Context(), dbgen.CreateUserParams{
			Email:           userInfo.Email,
			Name:            staffName,
			PasswordHash:    pgtype.Text{Valid: false},
			PhoneNumber:     invitation.PhoneNumber,
			PfpExternalUrl:  pfpParam,
			IsActive:        true,
			EmailVerifiedAt: pgtype.Timestamptz{Time: now, Valid: true},
		})
		if err != nil {
			h.deliverAuthError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create staff account", err)
			return
		}

		_, err = qtx.CreateStaff(r.Context(), dbgen.CreateStaffParams{
			UserID:     newUser.ID,
			DivisionID: invitation.DivisionID,
			Role:       invitation.Role,
		})
		if err != nil {
			h.deliverAuthError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to assign staff record", err)
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

		userResp, accessToken, rawRefreshToken, accessExpiry, err := h.issueAuthTokens(
			r.Context(), qtx, OAuthUserClaims{
				UserID:          newUser.ID,
				Email:           newUser.Email,
				Name:            newUser.Name,
				PhoneNumber:     newUser.PhoneNumber,
				IsActive:        newUser.IsActive,
				IsStaff:         true,
				Role:            string(invitation.Role),
				DivisionID:      invitation.DivisionID,
				DivisionName:    invitation.DivisionName,
				IsHero:          false,
				EmailVerifiedAt: newUser.EmailVerifiedAt,
			},
		)
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

	finalName := strings.TrimSpace(userInfo.Name)
	if finalName == "" {
		finalName = userInfo.Email
	}

	var pfpParam pgtype.Text
	if userInfo.Picture != "" {
		pfpParam = pgtype.Text{String: userInfo.Picture, Valid: true}
	}

	now := time.Now()
	newHeroUser, err := qtx.CreateUser(r.Context(), dbgen.CreateUserParams{
		Email:           userInfo.Email,
		Name:            finalName,
		PasswordHash:    pgtype.Text{Valid: false},
		PhoneNumber:     pgtype.Text{Valid: false},
		PfpExternalUrl:  pfpParam,
		IsActive:        true,
		EmailVerifiedAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		h.deliverAuthError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create hero user", err)
		return
	}

	_, err = qtx.CreateHero(r.Context(), dbgen.CreateHeroParams{
		UserID:     newHeroUser.ID,
		RemindMeAt: pgtype.Timestamptz{Valid: false},
	})
	if err != nil {
		h.deliverAuthError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create hero profile", err)
		return
	}

	_, err = qtx.CreateOAuthAccount(r.Context(), dbgen.CreateOAuthAccountParams{
		UserID:         newHeroUser.ID,
		Provider:       "google",
		ProviderUserID: userInfo.Sub,
		Email:          userInfo.Email,
	})
	if err != nil {
		h.deliverAuthError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to link Google account", err)
		return
	}

	parts := strings.Split(userInfo.Email, "@")
	if len(parts) == 2 {
		domain := strings.ToLower(strings.TrimSpace(parts[1]))
		matchedCat, matchErr := qtx.MatchUniversityCategoryByEmailDomain(r.Context(), domain)
		if matchErr == nil {
			_, _ = qtx.AddHeroCategory(r.Context(), dbgen.AddHeroCategoryParams{
				HeroID:                newHeroUser.ID,
				BeneficiaryCategoryID: matchedCat.ID,
			})
		}
	}

	userResp, accessToken, rawRefreshToken, accessExpiry, err := h.issueAuthTokens(
		r.Context(), qtx, OAuthUserClaims{
			UserID:          newHeroUser.ID,
			Email:           newHeroUser.Email,
			Name:            newHeroUser.Name,
			PhoneNumber:     newHeroUser.PhoneNumber,
			IsActive:        newHeroUser.IsActive,
			IsStaff:         false,
			Role:            "",
			DivisionID:      0,
			DivisionName:    "",
			IsHero:          true,
			EmailVerifiedAt: newHeroUser.EmailVerifiedAt,
		},
	)
	if err != nil {
		h.deliverAuthError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to issue authentication tokens", err)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		h.deliverAuthError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to commit transaction", err)
		return
	}

	h.deliverAuthTokens(w, r, accessToken, rawRefreshToken, accessExpiry, userResp)
}

func (h *OAuthHandler) issueAuthTokens(ctx context.Context, qtx *dbgen.Queries, claims OAuthUserClaims) (UserResponse, string, string, time.Duration, error) {
	session, err := IssueUserSession(ctx, qtx, h.config, UserSessionParams{
		UserID:     claims.UserID,
		Email:      claims.Email,
		IsStaff:    claims.IsStaff,
		Role:       claims.Role,
		DivisionID: claims.DivisionID,
		IsHero:     claims.IsHero,
	})
	if err != nil {
		return UserResponse{}, "", "", 0, fmt.Errorf("failed to issue session: %w", err)
	}

	userResp := ToUserResponse(
		claims.UserID,
		claims.Email,
		claims.Name,
		claims.PhoneNumber,
		claims.IsActive,
		claims.IsStaff,
		claims.Role,
		claims.DivisionID,
		claims.DivisionName,
		claims.IsHero,
		claims.EmailVerifiedAt,
	)

	return userResp, session.AccessToken, session.RefreshToken, session.AccessExpiry, nil
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
