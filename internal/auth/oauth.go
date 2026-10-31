package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	OAuthStateCookieName = "oauth_state"
	OAuthStateTTL        = 10 * time.Minute
	GoogleUserInfoURL    = "https://www.googleapis.com/oauth2/v3/userinfo"
)

type OAuthStatePayload struct {
	Nonce           string `json:"nonce"`
	InvitationToken string `json:"invitation_token,omitempty"`
	Timestamp       int64  `json:"timestamp"`
}

type GoogleUserInfo struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

func GenerateOAuthState(secret string, invitationToken string) (state string, nonce string, err error) {
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", "", fmt.Errorf("failed to generate random nonce: %w", err)
	}
	nonce = hex.EncodeToString(nonceBytes)

	payload := OAuthStatePayload{
		Nonce:           nonce,
		InvitationToken: invitationToken,
		Timestamp:       time.Now().Unix(),
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal state payload: %w", err)
	}

	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payloadB64))
	sigB64 := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	state = fmt.Sprintf("%s.%s", payloadB64, sigB64)
	return state, nonce, nil
}

func VerifyOAuthState(secret string, state string, cookieNonce string) (string, error) {
	if state == "" || cookieNonce == "" {
		return "", errors.New("missing state or cookie nonce")
	}

	var payloadB64, sigB64 string
	for i := 0; i < len(state); i++ {
		if state[i] == '.' {
			payloadB64 = state[:i]
			sigB64 = state[i+1:]
			break
		}
	}
	if payloadB64 == "" || sigB64 == "" {
		return "", errors.New("invalid state format")
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payloadB64))
	expectedSig := mac.Sum(nil)

	actualSig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return "", errors.New("invalid state signature encoding")
	}

	if !hmac.Equal(expectedSig, actualSig) {
		return "", errors.New("state signature verification failed")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return "", errors.New("invalid state payload encoding")
	}

	var payload OAuthStatePayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return "", errors.New("failed to parse state payload")
	}

	if time.Since(time.Unix(payload.Timestamp, 0)) > OAuthStateTTL {
		return "", errors.New("oauth state expired")
	}

	if payload.Nonce != cookieNonce {
		return "", errors.New("state nonce does not match cookie")
	}

	return payload.InvitationToken, nil
}

func SetOAuthStateCookie(w http.ResponseWriter, nonce string, isSecure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     OAuthStateCookieName,
		Value:    nonce,
		Path:     "/",
		MaxAge:   int(OAuthStateTTL.Seconds()),
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func ClearOAuthStateCookie(w http.ResponseWriter, isSecure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     OAuthStateCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func FetchGoogleUserInfo(ctx context.Context, client *http.Client, endpoint string) (*GoogleUserInfo, error) {
	if endpoint == "" {
		endpoint = GoogleUserInfoURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create userinfo request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call Google userinfo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("Google userinfo returned status %d: %s", resp.StatusCode, string(body))
	}

	var userInfo GoogleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return nil, fmt.Errorf("failed to decode Google userinfo response: %w", err)
	}

	if userInfo.Sub == "" || userInfo.Email == "" {
		return nil, errors.New("Google userinfo response missing required sub or email")
	}

	return &userInfo, nil
}
