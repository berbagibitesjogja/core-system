package handler

//
// import (
// 	"encoding/json"
// 	"errors"
// 	"net/http"
// 	"strings"
// 	"time"
//
// 	"github.com/jackc/pgx/v5"
// 	"github.com/jackc/pgx/v5/pgxpool"
//
// 	"github.com/berbagibitesjogja/core-system/internal/auth"
// 	"github.com/berbagibitesjogja/core-system/internal/config"
// 	"github.com/berbagibitesjogja/core-system/internal/dbgen"
// 	"github.com/berbagibitesjogja/core-system/internal/response"
// )
//
// type AuthHandler struct {
// 	pool    *pgxpool.Pool
// 	cfg     *config.Config
// 	queries *dbgen.Queries
// }
//
// func NewAuthHandler(pool *pgxpool.Pool, cfg *config.Config) *AuthHandler {
// 	return &AuthHandler{
// 		pool:    pool,
// 		cfg:     cfg,
// 		queries: dbgen.New(pool),
// 	}
// }
//
// type LoginRequest struct {
// 	Username string `json:"username"`
// 	Password string `json:"password"`
// }
//
// type UserLoginRequest struct {
// 	Email    string `json:"email"`
// 	Password string `json:"password"`
// }
//
// type RefreshRequest struct {
// 	RefreshToken string `json:"refresh_token"`
// }
//
// type TokenResponse struct {
// 	AccessToken  string `json:"access_token"`
// 	RefreshToken string `json:"refresh_token"`
// 	ExpiresIn    int64  `json:"expires_in"`
// }
//
// type UserResponse struct {
// 	ID              int64   `json:"id"`
// 	Role            string  `json:"role"`
// 	Name            string  `json:"name"`
// 	Email           string  `json:"email"`
// 	Grade           int32   `json:"grade"`
// 	IsVerified      bool    `json:"is_verified"`
// 	EmailVerifiedAt *string `json:"email_verified_at,omitempty"`
// 	PhoneNumber     *string `json:"phone_number,omitempty"`
// 	Nim             *string `json:"nim,omitempty"`
// }
//
// type UserTokenResponse struct {
// 	AccessToken  string       `json:"access_token"`
// 	RefreshToken string       `json:"refresh_token"`
// 	ExpiresIn    int64        `json:"expires_in"`
// 	User         UserResponse `json:"user"`
// }
//
// func (h *AuthHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
// 	var req LoginRequest
// 	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
// 		response.BadRequest(w, "VAL_001", "Invalid request body", nil)
// 		return
// 	}
//
// 	req.Username = strings.TrimSpace(req.Username)
// 	req.Password = strings.TrimSpace(req.Password)
//
// 	fieldErrors := make(map[string]string)
// 	if req.Username == "" {
// 		fieldErrors["username"] = "Username is required"
// 	}
// 	if req.Password == "" {
// 		fieldErrors["password"] = "Password is required"
// 	}
// 	if len(fieldErrors) > 0 {
// 		response.BadRequest(w, "VAL_001", "Validation failed", fieldErrors)
// 		return
// 	}
//
// 	admin, err := h.queries.GetAdminByUsername(r.Context(), req.Username)
// 	if err != nil {
// 		if errors.Is(err, pgx.ErrNoRows) {
// 			response.Unauthorized(w, "Invalid username or password")
// 			return
// 		}
// 		response.InternalServerError(w, r, err, "Failed to look up admin")
// 		return
// 	}
//
// 	if !admin.PasswordHash.Valid || !auth.CheckPassword(req.Password, admin.PasswordHash.String) {
// 		response.Unauthorized(w, "Invalid username or password")
// 		return
// 	}
//
// 	tokenResponse, err := h.generateAdminTokenPair(r, admin.ID, admin.Username)
// 	if err != nil {
// 		response.InternalServerError(w, r, err, "Failed to generate token pair")
// 		return
// 	}
//
// 	response.Success(w, http.StatusOK, "Login successful", tokenResponse)
// }
//
// func (h *AuthHandler) RefreshTokenHandler(w http.ResponseWriter, r *http.Request) {
// 	var req RefreshRequest
// 	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
// 		response.BadRequest(w, "VAL_001", "Invalid request body", nil)
// 		return
// 	}
//
// 	req.RefreshToken = strings.TrimSpace(req.RefreshToken)
// 	if req.RefreshToken == "" {
// 		response.BadRequest(w, "VAL_001", "Validation failed", map[string]string{
// 			"refresh_token": "Refresh token is required",
// 		})
// 		return
// 	}
//
// 	refreshTokenHash := auth.HashToken(req.RefreshToken)
// 	tokenRecord, err := h.queries.GetAdminRefreshTokenByHash(r.Context(), refreshTokenHash)
// 	if err != nil {
// 		if errors.Is(err, pgx.ErrNoRows) {
// 			response.Unauthorized(w, "Invalid refresh token")
// 			return
// 		}
// 		response.InternalServerError(w, r, err, "Failed to look up refresh token")
// 		return
// 	}
//
// 	if tokenRecord.RevokedAt.Valid {
// 		_ = h.queries.RevokeAllAdminTokens(r.Context(), tokenRecord.AdminID)
// 		response.Unauthorized(w, "Refresh token has been revoked")
// 		return
// 	}
//
// 	if time.Now().After(tokenRecord.ExpiresAt) {
// 		response.Unauthorized(w, "Refresh token has expired")
// 		return
// 	}
//
// 	if err := h.queries.RevokeAdminRefreshToken(r.Context(), refreshTokenHash); err != nil {
// 		response.InternalServerError(w, r, err, "Failed to revoke refresh token")
// 		return
// 	}
//
// 	admin, err := h.queries.GetAdminByID(r.Context(), tokenRecord.AdminID)
// 	if err != nil {
// 		response.InternalServerError(w, r, err, "Failed to look up admin")
// 		return
// 	}
//
// 	tokenResponse, err := h.generateAdminTokenPair(r, admin.ID, admin.Username)
// 	if err != nil {
// 		response.InternalServerError(w, r, err, "Failed to generate token pair")
// 		return
// 	}
//
// 	response.Success(w, http.StatusOK, "Token refreshed successfully", tokenResponse)
// }
//
// func (h *AuthHandler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
// 	var req RefreshRequest
// 	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
// 		response.BadRequest(w, "VAL_001", "Invalid request body", nil)
// 		return
// 	}
//
// 	req.RefreshToken = strings.TrimSpace(req.RefreshToken)
// 	if req.RefreshToken != "" {
// 		_ = h.queries.RevokeAdminRefreshToken(r.Context(), auth.HashToken(req.RefreshToken))
// 	}
//
// 	response.Success(w, http.StatusOK, "Logged out successfully", nil)
// }
//
// func (h *AuthHandler) UserLoginHandler(w http.ResponseWriter, r *http.Request) {
// 	var req UserLoginRequest
// 	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
// 		response.BadRequest(w, "VAL_001", "Invalid request body", nil)
// 		return
// 	}
//
// 	req.Email = strings.TrimSpace(req.Email)
// 	req.Password = strings.TrimSpace(req.Password)
//
// 	fieldErrors := make(map[string]string)
// 	if req.Email == "" {
// 		fieldErrors["email"] = "Email is required"
// 	}
// 	if req.Password == "" {
// 		fieldErrors["password"] = "Password is required"
// 	}
// 	if len(fieldErrors) > 0 {
// 		response.BadRequest(w, "VAL_001", "Validation failed", fieldErrors)
// 		return
// 	}
//
// 	user, err := h.queries.GetUserByEmail(r.Context(), req.Email)
// 	if err != nil {
// 		if errors.Is(err, pgx.ErrNoRows) {
// 			response.Unauthorized(w, "Invalid email or password")
// 			return
// 		}
// 		response.InternalServerError(w, r, err, "Failed to look up user")
// 		return
// 	}
//
// 	if !user.PasswordHash.Valid || !auth.CheckPassword(req.Password, user.PasswordHash.String) {
// 		response.Unauthorized(w, "Invalid email or password")
// 		return
// 	}
//
// 	if !user.EmailVerifiedAt.Valid {
// 		response.Error(w, http.StatusForbidden, "EMAIL_NOT_VERIFIED", "Please verify your email before logging in", nil)
// 		return
// 	}
//
// 	tokenResponse, err := h.generateUserTokenPair(r, user)
// 	if err != nil {
// 		response.InternalServerError(w, r, err, "Failed to generate token pair")
// 		return
// 	}
//
// 	response.Success(w, http.StatusOK, "Login successful", tokenResponse)
// }
//
// func (h *AuthHandler) UserRefreshTokenHandler(w http.ResponseWriter, r *http.Request) {
// 	var req RefreshRequest
// 	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
// 		response.BadRequest(w, "VAL_001", "Invalid request body", nil)
// 		return
// 	}
//
// 	req.RefreshToken = strings.TrimSpace(req.RefreshToken)
// 	if req.RefreshToken == "" {
// 		response.BadRequest(w, "VAL_001", "Validation failed", map[string]string{
// 			"refresh_token": "Refresh token is required",
// 		})
// 		return
// 	}
//
// 	refreshTokenHash := auth.HashToken(req.RefreshToken)
// 	tokenRecord, err := h.queries.GetUserRefreshTokenByHash(r.Context(), refreshTokenHash)
// 	if err != nil {
// 		if errors.Is(err, pgx.ErrNoRows) {
// 			response.Unauthorized(w, "Invalid refresh token")
// 			return
// 		}
// 		response.InternalServerError(w, r, err, "Failed to look up refresh token")
// 		return
// 	}
//
// 	if tokenRecord.RevokedAt.Valid {
// 		response.Unauthorized(w, "Refresh token has been revoked")
// 		return
// 	}
//
// 	if time.Now().After(tokenRecord.ExpiresAt) {
// 		response.Unauthorized(w, "Refresh token has expired")
// 		return
// 	}
//
// 	if err := h.queries.RevokeUserRefreshToken(r.Context(), refreshTokenHash); err != nil {
// 		response.InternalServerError(w, r, err, "Failed to revoke refresh token")
// 		return
// 	}
//
// 	user, err := h.queries.GetUserByID(r.Context(), tokenRecord.UserID)
// 	if err != nil {
// 		response.InternalServerError(w, r, err, "Failed to look up user")
// 		return
// 	}
//
// 	tokenResponse, err := h.generateUserTokenPairFromUser(r, user)
// 	if err != nil {
// 		response.InternalServerError(w, r, err, "Failed to generate token pair")
// 		return
// 	}
//
// 	response.Success(w, http.StatusOK, "Token refreshed successfully", tokenResponse)
// }
//
// func (h *AuthHandler) UserLogoutHandler(w http.ResponseWriter, r *http.Request) {
// 	var req RefreshRequest
// 	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
// 		response.BadRequest(w, "VAL_001", "Invalid request body", nil)
// 		return
// 	}
//
// 	req.RefreshToken = strings.TrimSpace(req.RefreshToken)
// 	if req.RefreshToken != "" {
// 		_ = h.queries.RevokeUserRefreshToken(r.Context(), auth.HashToken(req.RefreshToken))
// 	}
//
// 	response.Success(w, http.StatusOK, "Logged out successfully", nil)
// }
//
// func (h *AuthHandler) generateAdminTokenPair(r *http.Request, adminID int64, username string) (*TokenResponse, error) {
// 	accessExpiry := h.cfg.AppConfig.JWTAccessExpiry
// 	accessToken, err := auth.SignAccessToken(adminID, username, "", "admin", h.cfg.AppConfig.JWTSecret, accessExpiry)
// 	if err != nil {
// 		return nil, err
// 	}
//
// 	rawRefreshToken, err := auth.GenerateRefreshToken()
// 	if err != nil {
// 		return nil, err
// 	}
//
// 	refreshExpiry := time.Now().Add(30 * 24 * time.Hour)
// 	err = h.queries.InsertAdminRefreshToken(r.Context(), dbgen.InsertAdminRefreshTokenParams{
// 		AdminID:   adminID,
// 		TokenHash: auth.HashToken(rawRefreshToken),
// 		ExpiresAt: refreshExpiry,
// 	})
// 	if err != nil {
// 		return nil, err
// 	}
//
// 	return &TokenResponse{
// 		AccessToken:  accessToken,
// 		RefreshToken: rawRefreshToken,
// 		ExpiresIn:    int64(accessExpiry.Seconds()),
// 	}, nil
// }
//
// func (h *AuthHandler) generateUserTokenPair(r *http.Request, user dbgen.GetUserByEmailRow) (*UserTokenResponse, error) {
// 	accessExpiry := h.cfg.AppConfig.JWTAccessExpiry
// 	accessToken, err := auth.SignAccessToken(user.ID, "", user.Email, string(user.Role), h.cfg.AppConfig.JWTSecret, accessExpiry)
// 	if err != nil {
// 		return nil, err
// 	}
//
// 	rawRefreshToken, err := auth.GenerateRefreshToken()
// 	if err != nil {
// 		return nil, err
// 	}
//
// 	refreshExpiry := time.Now().Add(h.cfg.AppConfig.JWTRefreshExpiry)
// 	err = h.queries.InsertUserRefreshToken(r.Context(), dbgen.InsertUserRefreshTokenParams{
// 		UserID:    user.ID,
// 		TokenHash: auth.HashToken(rawRefreshToken),
// 		ExpiresAt: refreshExpiry,
// 	})
// 	if err != nil {
// 		return nil, err
// 	}
//
// 	var phonePtr *string
// 	if user.PhoneNumber.Valid {
// 		phonePtr = &user.PhoneNumber.String
// 	}
// 	var nimPtr *string
// 	if user.Nim.Valid {
// 		nimPtr = &user.Nim.String
// 	}
//
// 	var verifiedAtPtr *string
// 	if user.EmailVerifiedAt.Valid {
// 		formatted := user.EmailVerifiedAt.Time.Format(time.RFC3339)
// 		verifiedAtPtr = &formatted
// 	}
//
// 	return &UserTokenResponse{
// 		AccessToken:  accessToken,
// 		RefreshToken: rawRefreshToken,
// 		ExpiresIn:    int64(accessExpiry.Seconds()),
// 		User: UserResponse{
// 			ID:              user.ID,
// 			Role:            string(user.Role),
// 			Name:            user.Name,
// 			Email:           user.Email,
// 			Grade:           user.Grade,
// 			PhoneNumber:     phonePtr,
// 			Nim:             nimPtr,
// 			IsVerified:      user.EmailVerifiedAt.Valid,
// 			EmailVerifiedAt: verifiedAtPtr,
// 		},
// 	}, nil
// }
//
// func (h *AuthHandler) generateUserTokenPairFromUser(r *http.Request, user dbgen.GetUserByIDRow) (*UserTokenResponse, error) {
// 	accessExpiry := h.cfg.AppConfig.JWTAccessExpiry
// 	accessToken, err := auth.SignAccessToken(user.ID, "", user.Email, string(user.Role), h.cfg.AppConfig.JWTSecret, accessExpiry)
// 	if err != nil {
// 		return nil, err
// 	}
//
// 	rawRefreshToken, err := auth.GenerateRefreshToken()
// 	if err != nil {
// 		return nil, err
// 	}
//
// 	refreshExpiry := time.Now().Add(h.cfg.AppConfig.JWTRefreshExpiry)
// 	err = h.queries.InsertUserRefreshToken(r.Context(), dbgen.InsertUserRefreshTokenParams{
// 		UserID:    user.ID,
// 		TokenHash: auth.HashToken(rawRefreshToken),
// 		ExpiresAt: refreshExpiry,
// 	})
// 	if err != nil {
// 		return nil, err
// 	}
//
// 	var phonePtr *string
// 	if user.PhoneNumber.Valid {
// 		phonePtr = &user.PhoneNumber.String
// 	}
// 	var nimPtr *string
// 	if user.Nim.Valid {
// 		nimPtr = &user.Nim.String
// 	}
//
// 	var verifiedAtPtr *string
// 	if user.EmailVerifiedAt.Valid {
// 		formatted := user.EmailVerifiedAt.Time.Format(time.RFC3339)
// 		verifiedAtPtr = &formatted
// 	}
//
// 	return &UserTokenResponse{
// 		AccessToken:  accessToken,
// 		RefreshToken: rawRefreshToken,
// 		ExpiresIn:    int64(accessExpiry.Seconds()),
// 		User: UserResponse{
// 			ID:              user.ID,
// 			Role:            string(user.Role),
// 			Name:            user.Name,
// 			Email:           user.Email,
// 			Grade:           user.Grade,
// 			PhoneNumber:     phonePtr,
// 			Nim:             nimPtr,
// 			IsVerified:      user.EmailVerifiedAt.Valid,
// 			EmailVerifiedAt: verifiedAtPtr,
// 		},
// 	}, nil
// }
