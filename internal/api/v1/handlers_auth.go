package apiv1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/sumia01/media-gate/internal/auth"
	"github.com/sumia01/media-gate/internal/settings"
	"github.com/sumia01/media-gate/internal/store"
)

// Types for manual auth handlers (not in OpenAPI spec / generated code).
type LoginRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	RememberMe *bool  `json:"rememberMe,omitempty"`
}

type LoginResponse struct {
	AccessToken string      `json:"accessToken"`
	User        UserProfile `json:"user"`
}

type RefreshResponse struct {
	AccessToken string `json:"accessToken"`
}

// --- Manual HTTP handlers (need cookie access) ---

// LoginHandler handles POST /api/v1/auth/login.
func (h *Handlers) LoginHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Code: 400, Message: "invalid request body"})
			return
		}

		user, err := h.authSvc.Authenticate(req.Email, req.Password)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, ErrorResponse{Code: 401, Message: "invalid email or password"})
			return
		}

		accessToken, err := h.authSvc.GenerateAccessToken(user)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Code: 500, Message: "failed to generate token"})
			return
		}

		rememberMe := req.RememberMe != nil && *req.RememberMe
		refreshToken, err := h.authSvc.GenerateRefreshToken(user.ID, rememberMe)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Code: 500, Message: "failed to generate refresh token"})
			return
		}

		setRefreshCookie(w, r, refreshToken.Token, h.authSvc.RefreshTTL(rememberMe))

		resp := LoginResponse{
			AccessToken: accessToken,
			User:        userToAPI(user),
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// RefreshHandler handles POST /api/v1/auth/refresh.
func (h *Handlers) RefreshHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("refresh_token")
		if err != nil || cookie.Value == "" {
			writeJSON(w, http.StatusUnauthorized, ErrorResponse{Code: 401, Message: "no refresh token"})
			return
		}

		newRT, user, err := h.authSvc.RotateRefreshToken(cookie.Value)
		if err != nil {
			clearRefreshCookie(w, r)
			writeJSON(w, http.StatusUnauthorized, ErrorResponse{Code: 401, Message: "invalid refresh token"})
			return
		}

		accessToken, err := h.authSvc.GenerateAccessToken(user)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Code: 500, Message: "failed to generate token"})
			return
		}

		ttl := time.Until(newRT.ExpiresAt)
		setRefreshCookie(w, r, newRT.Token, ttl)

		writeJSON(w, http.StatusOK, RefreshResponse{AccessToken: accessToken})
	}
}

// LogoutHandler handles POST /api/v1/auth/logout.
func (h *Handlers) LogoutHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie("refresh_token"); err == nil && cookie.Value != "" {
			_ = h.authSvc.RevokeRefreshToken(cookie.Value)
		}
		clearRefreshCookie(w, r)
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- Strict server handler methods ---

func (h *Handlers) GetMyProfile(ctx context.Context, _ GetMyProfileRequestObject) (GetMyProfileResponseObject, error) {
	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		return GetMyProfile200JSONResponse{}, errors.New("unauthorized")
	}
	user, err := h.authSvc.GetUser(userID)
	if err != nil {
		return GetMyProfile200JSONResponse{}, err
	}
	return GetMyProfile200JSONResponse(userToAPI(user)), nil
}

func (h *Handlers) UpdateMyProfile(ctx context.Context, req UpdateMyProfileRequestObject) (UpdateMyProfileResponseObject, error) {
	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		return UpdateMyProfile200JSONResponse{}, errors.New("unauthorized")
	}

	firstName := ""
	if req.Body.FirstName != nil {
		firstName = *req.Body.FirstName
	}
	lastName := ""
	if req.Body.LastName != nil {
		lastName = *req.Body.LastName
	}

	user, err := h.authSvc.UpdateProfile(userID, firstName, lastName, req.Body.BirthYear)
	if err != nil {
		return UpdateMyProfile200JSONResponse{}, err
	}
	return UpdateMyProfile200JSONResponse(userToAPI(user)), nil
}

func (h *Handlers) ChangePassword(ctx context.Context, req ChangePasswordRequestObject) (ChangePasswordResponseObject, error) {
	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		return ChangePassword400JSONResponse{Code: 401, Message: "unauthorized"}, nil
	}

	if err := h.authSvc.ChangePassword(userID, req.Body.OldPassword, req.Body.NewPassword); err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			return ChangePassword400JSONResponse{Code: 400, Message: "incorrect old password"}, nil
		}
		return ChangePassword400JSONResponse{Code: 500, Message: "failed to change password"}, nil
	}

	user, err := h.authSvc.GetUser(userID)
	if err != nil {
		return ChangePassword400JSONResponse{Code: 500, Message: "failed to get user"}, nil
	}
	return ChangePassword200JSONResponse(userToAPI(user)), nil
}

func (h *Handlers) ListUsers(_ context.Context, _ ListUsersRequestObject) (ListUsersResponseObject, error) {
	users, err := h.authSvc.ListUsers()
	if err != nil {
		return nil, err
	}
	profiles := make([]UserProfile, len(users))
	for i := range users {
		profiles[i] = userToAPI(&users[i])
	}
	return ListUsers200JSONResponse{Users: profiles}, nil
}

func (h *Handlers) RegisterUser(_ context.Context, req RegisterUserRequestObject) (RegisterUserResponseObject, error) {
	firstName := ""
	if req.Body.FirstName != nil {
		firstName = *req.Body.FirstName
	}
	lastName := ""
	if req.Body.LastName != nil {
		lastName = *req.Body.LastName
	}

	user, err := h.authSvc.Register(string(req.Body.Email), req.Body.Password, firstName, lastName, req.Body.BirthYear)
	if err != nil {
		if errors.Is(err, auth.ErrUserExists) {
			return RegisterUser409JSONResponse{Code: 409, Message: "user with this email already exists"}, nil
		}
		return RegisterUser400JSONResponse{Code: 400, Message: err.Error()}, nil
	}
	return RegisterUser201JSONResponse(userToAPI(user)), nil
}

func (h *Handlers) DeleteUser(_ context.Context, req DeleteUserRequestObject) (DeleteUserResponseObject, error) {
	if err := h.authSvc.DeleteUser(uint(req.Id)); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return DeleteUser404JSONResponse{Code: 404, Message: "user not found"}, nil
		}
		return DeleteUser404JSONResponse{Code: 500, Message: "failed to delete user"}, nil
	}
	return DeleteUser204Response{}, nil
}

// --- Setup (first-run) handlers ---

type SetupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type SetupStatusResponse struct {
	NeedsSetup          bool `json:"needsSetup"`
	OnboardingCompleted bool `json:"onboardingCompleted"`
	OnboardingStep      int  `json:"onboardingStep"`
}

// SetupStatusHandler handles GET /api/v1/setup/status (unauthenticated).
func (h *Handlers) SetupStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		count, err := h.authSvc.CountUsers()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Code: 500, Message: "failed to check users"})
			return
		}

		needsSetup := count == 0
		onboardingCompleted := false
		onboardingStep := 0

		if !needsSetup {
			// Existing installation without explicit onboarding setting → treat as completed.
			completedVal := h.settings.GetWithDefault(settings.KeyOnboardingCompleted, "true")
			onboardingCompleted = completedVal == "true"

			stepVal := h.settings.GetWithDefault(settings.KeyOnboardingStep, "0")
			if n, err := strconv.Atoi(stepVal); err == nil {
				onboardingStep = n
			}
		}

		writeJSON(w, http.StatusOK, SetupStatusResponse{
			NeedsSetup:          needsSetup,
			OnboardingCompleted: onboardingCompleted,
			OnboardingStep:      onboardingStep,
		})
	}
}

// SetupHandler handles POST /api/v1/auth/setup (unauthenticated, first-user creation).
func (h *Handlers) SetupHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		count, err := h.authSvc.CountUsers()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Code: 500, Message: "failed to check users"})
			return
		}
		if count > 0 {
			writeJSON(w, http.StatusForbidden, ErrorResponse{Code: 403, Message: "setup already completed"})
			return
		}

		var req SetupRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Code: 400, Message: "invalid request body"})
			return
		}

		user, err := h.authSvc.Register(req.Email, req.Password, "", "", nil)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Code: 400, Message: err.Error()})
			return
		}

		accessToken, err := h.authSvc.GenerateAccessToken(user)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Code: 500, Message: "failed to generate token"})
			return
		}

		refreshToken, err := h.authSvc.GenerateRefreshToken(user.ID, true)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Code: 500, Message: "failed to generate refresh token"})
			return
		}

		setRefreshCookie(w, r, refreshToken.Token, h.authSvc.RefreshTTL(true))

		// Record that the first onboarding step is done.
		_ = h.settings.Update([]settings.KeyValue{
			{Key: settings.KeyOnboardingStep, Value: "1"},
			{Key: settings.KeyOnboardingCompleted, Value: "false"},
		})

		writeJSON(w, http.StatusOK, LoginResponse{
			AccessToken: accessToken,
			User:        userToAPI(user),
		})
	}
}

// --- Helpers ---

func setRefreshCookie(w http.ResponseWriter, r *http.Request, token string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    token,
		Path:     "/api/v1/auth",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(ttl.Seconds()),
	})
}

func clearRefreshCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Path:     "/api/v1/auth",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
