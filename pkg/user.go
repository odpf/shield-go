package pkg

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/odpf/shield/pkg/server/consts"
	shieldv1beta1 "github.com/odpf/shield/proto/v1beta1"
	"net/http"
	"net/url"
	"strings"
)

const (
	DefaultUserTokenHeader = consts.UserTokenRequestKey
	DefaultSessionID       = consts.SessionRequestKey
)

func GetAuthenticatedUser(r *http.Request, httpClient HTTPClient, shieldHost *url.URL, keySet jwk.Set) (*shieldv1beta1.User, map[string]any, string, error) {
	// check if context token is present
	userToken := strings.TrimSpace(r.Header.Get(DefaultUserTokenHeader))
	authHeader := r.Header.Get("authorization")
	if authHeader != "" {
		// check for bearer token, if present use that as user token
		if strings.HasPrefix(authHeader, "Bearer ") {
			userToken = strings.TrimPrefix(authHeader, "Bearer ")
		}
	}
	if userToken != "" {
		// if present, verify token
		claims, err := GetTokenClaims(r.Context(), keySet, userToken)
		if err != nil {
			return nil, nil, "", err
		}
		return GetUserFromClaims(claims), claims, userToken, nil
	}

	// check for session cookie
	sessionCookie, err := r.Cookie(DefaultSessionID)
	if err != nil || sessionCookie.Valid() != nil {
		return nil, nil, "", ErrInvalidHeader
	}
	// going via session route is slower then token route, but it also fetches full user profile
	u, userToken, err := GetUserProfile(r.Context(), httpClient, shieldHost, r.Header)
	if err != nil {
		return nil, nil, "", fmt.Errorf("%s : %w", ErrInvalidSession.Error(), err)
	}
	claims, err := GetTokenClaims(r.Context(), keySet, userToken)
	if err != nil {
		return nil, nil, "", fmt.Errorf("%s : %w", ErrInvalidSession.Error(), err)
	}
	return u, claims, userToken, nil
}

// GetTokenClaims parse & verify jwt with shield public keys
func GetTokenClaims(ctx context.Context, keySet jwk.Set, userToken string) (map[string]any, error) {
	// verify token with jwks
	verifiedToken, err := jwt.Parse([]byte(userToken), jwt.WithKeySet(keySet))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ErrInvalidToken.Error(), err)
	}
	tokenClaims, err := verifiedToken.AsMap(ctx)
	if err != nil {
		return nil, err
	}
	return tokenClaims, nil
}

func GetUserFromClaims(claims map[string]any) *shieldv1beta1.User {
	u := &shieldv1beta1.User{
		Id: claims["sub"].(string),
	}
	if val, ok := claims["email"]; ok {
		u.Email = val.(string)
	}
	if val, ok := claims["name"]; ok {
		u.Name = val.(string)
	}
	return u
}

// GetUserProfile fetches profile of authorized user from shield server
func GetUserProfile(ctx context.Context, client HTTPClient, shieldHost *url.URL, headers http.Header) (*shieldv1beta1.User, string, error) {
	getUserRequest, err := http.NewRequestWithContext(ctx, http.MethodGet,
		shieldHost.ResolveReference(&url.URL{Path: CurrentUserProfilePath}).String(), nil)
	if err != nil {
		return nil, "", err
	}
	getUserRequest.Header = headers
	resp, err := client.Do(getUserRequest)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", ErrInternalServer
	}
	currentUserResp := &shieldv1beta1.GetCurrentUserResponse{}
	if err := json.NewDecoder(resp.Body).Decode(&currentUserResp); err != nil {
		return nil, "", err
	}
	userToken := resp.Header.Get(consts.UserTokenRequestKey)
	return currentUserResp.GetUser(), userToken, nil
}
