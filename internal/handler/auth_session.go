package handler

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/berbagibitesjogja/core-system/internal/auth"
	"github.com/berbagibitesjogja/core-system/internal/config"
	"github.com/berbagibitesjogja/core-system/internal/dbgen"
)

type UserSessionParams struct {
	UserID     uuid.UUID
	Email      string
	IsStaff    bool
	Role       string
	DivisionID int32
	IsHero     bool
}

type AuthSession struct {
	AccessToken  string
	RefreshToken string
	AccessExpiry time.Duration
}

func getAccessExpiry(cfg *config.Config) time.Duration {
	if cfg != nil && cfg.AppConfig.JWTAccessExpiry > 0 {
		return cfg.AppConfig.JWTAccessExpiry
	}
	return 15 * time.Minute
}

func getRefreshExpiry(cfg *config.Config) time.Duration {
	if cfg != nil && cfg.AppConfig.JWTRefreshExpiry > 0 {
		return cfg.AppConfig.JWTRefreshExpiry
	}
	return 7 * 24 * time.Hour
}

func IssueUserSession(ctx context.Context, qtx *dbgen.Queries, cfg *config.Config, params UserSessionParams) (*AuthSession, error) {
	accessExpiry := getAccessExpiry(cfg)
	refreshExpiry := getRefreshExpiry(cfg)

	jwtSecret := ""
	if cfg != nil {
		jwtSecret = cfg.AppConfig.JWTSecret
	}

	accessToken, err := auth.SignAccessToken(
		params.UserID,
		params.Email,
		params.IsStaff,
		params.Role,
		params.DivisionID,
		params.IsHero,
		jwtSecret,
		accessExpiry,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to sign access token: %w", err)
	}

	rawRefreshToken, refreshTokenHash, err := auth.GenerateSecureToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	now := time.Now()
	_, err = qtx.CreateRefreshToken(ctx, dbgen.CreateRefreshTokenParams{
		UserID:    params.UserID,
		TokenHash: refreshTokenHash,
		ExpiresAt: now.Add(refreshExpiry),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to record refresh token: %w", err)
	}

	return &AuthSession{
		AccessToken:  accessToken,
		RefreshToken: rawRefreshToken,
		AccessExpiry: accessExpiry,
	}, nil
}

func ToUserResponse(
	id uuid.UUID,
	email string,
	name string,
	phone pgtype.Text,
	isActive bool,
	isStaff bool,
	role string,
	divisionID int32,
	divisionName string,
	isHero bool,
	emailVerifiedAt pgtype.Timestamptz,
) UserResponse {
	var verifiedAtStr *string
	if emailVerifiedAt.Valid {
		formatted := emailVerifiedAt.Time.Format(time.RFC3339)
		verifiedAtStr = &formatted
	}

	return UserResponse{
		ID:              id.String(),
		Email:           email,
		Name:            name,
		PhoneNumber:     phone.String,
		IsActive:        isActive,
		IsStaff:         isStaff,
		Role:            role,
		DivisionID:      divisionID,
		DivisionName:    divisionName,
		IsHero:          isHero,
		EmailVerifiedAt: verifiedAtStr,
	}
}

func UserResponseFromRolesByID(u dbgen.GetUserWithRolesByIDRow) UserResponse {
	var roleStr, divNameStr string
	var divID int32
	if u.IsStaff && u.StaffRole.Valid {
		roleStr = string(u.StaffRole.UserRole)
		divID = u.StaffDivisionID.Int32
		if u.StaffDivisionName.Valid {
			divNameStr = u.StaffDivisionName.String
		}
	}
	return ToUserResponse(
		u.ID, u.Email, u.Name, u.PhoneNumber, u.IsActive,
		u.IsStaff, roleStr, divID, divNameStr,
		u.IsHero, u.EmailVerifiedAt,
	)
}

func UserResponseFromRolesByEmail(u dbgen.GetUserWithRolesByEmailRow) UserResponse {
	var roleStr, divNameStr string
	var divID int32
	if u.IsStaff && u.StaffRole.Valid {
		roleStr = string(u.StaffRole.UserRole)
		divID = u.StaffDivisionID.Int32
		if u.StaffDivisionName.Valid {
			divNameStr = u.StaffDivisionName.String
		}
	}
	return ToUserResponse(
		u.ID, u.Email, u.Name, u.PhoneNumber, u.IsActive,
		u.IsStaff, roleStr, divID, divNameStr,
		u.IsHero, u.EmailVerifiedAt,
	)
}

func ExtractStaffClaims(isStaff bool, role dbgen.NullUserRole, divID pgtype.Int4) (roleStr string, divisionID int32) {
	if isStaff && role.Valid {
		return string(role.UserRole), divID.Int32
	}
	return "", 0
}
