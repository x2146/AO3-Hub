package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"golang.org/x/crypto/argon2"
	"strconv"
)

const cookieName = "ao3hub_session"

const dummyPasswordHash = "$argon2id$v=19$m=65536,t=2,p=1$9Gh1wrVfGclAkVQCJJXAh1BDCdF9R+CGKEDhBb3cGuQ$Oo+nmvbwbz/X960SLZrw6nLFeroEzz3DU+Uz3YLNk4k"

type contextKey string

const userContextKey contextKey = "user"

func hashPassword(plain string) (string, error) {
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	timeCost := uint32(2)
	memory := uint32(65536)
	threads := uint8(1)
	keyLen := uint32(32)
	hash := argon2.IDKey([]byte(plain), salt, timeCost, memory, threads, keyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf(
		"$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		memory,
		timeCost,
		threads,
		b64.EncodeToString(salt),
		b64.EncodeToString(hash),
	), nil
}

func verifyPassword(plain, encoded string) bool {
	if encoded == "" {
		return false
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}
	params := strings.Split(parts[3], ",")
	var memory uint64
	var timeCost uint64
	var threads uint64
	for _, p := range params {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			return false
		}
		n, err := strconv.ParseUint(kv[1], 10, 32)
		if err != nil {
			return false
		}
		switch kv[0] {
		case "m":
			memory = n
		case "t":
			timeCost = n
		case "p":
			threads = n
		}
	}
	if memory < 8*1024 || memory > 256*1024 || timeCost < 1 || timeCost > 10 || threads < 1 || threads > 16 {
		return false
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return false
	}
	expected, err := b64.DecodeString(parts[5])
	if err != nil || len(expected) < 16 || len(expected) > 64 {
		return false
	}
	actual := argon2.IDKey([]byte(plain), salt, uint32(timeCost), uint32(memory), uint8(threads), uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func (a *App) sessionTTL() (time.Duration, error) {
	cfg, err := a.store.LoadConfig()
	if err != nil {
		return 0, err
	}
	if cfg.Auth.SessionTTLDays <= 0 {
		return 30 * 24 * time.Hour, nil
	}
	return time.Duration(cfg.Auth.SessionTTLDays) * 24 * time.Hour, nil
}

func (a *App) startSession(w http.ResponseWriter, r *http.Request, userID string) error {
	ttl, err := a.sessionTTL()
	if err != nil {
		return err
	}
	session, err := a.store.CreateSession(userID, ttl)
	if err != nil {
		return err
	}
	a.writeSessionCookie(w, r, session.Token, ttl)
	return nil
}

func (a *App) endSession(w http.ResponseWriter, r *http.Request) error {
	if cookie, err := r.Cookie(cookieName); err == nil {
		if err := a.store.RemoveSession(cookie.Value); err != nil {
			return err
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (a *App) writeSessionCookie(w http.ResponseWriter, r *http.Request, token string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   a.effectiveRequestScheme(r) == "https",
	})
}

func (a *App) effectiveRequestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if a.publicOriginHost != "" && strings.EqualFold(r.Host, a.publicOriginHost) {
		proto := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("x-forwarded-proto"), ",")[0]))
		if proto == a.publicOriginScheme {
			return proto
		}
	}
	return "http"
}

func (a *App) resolveUser(w http.ResponseWriter, r *http.Request) (*UserRecord, error) {
	cookie, err := r.Cookie(cookieName)
	if err != nil || cookie.Value == "" {
		return nil, nil
	}
	session, err := a.store.FindValidSession(cookie.Value)
	if err != nil {
		return nil, err
	}
	if session == nil {
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
		return nil, nil
	}
	user, err := a.store.FindUserByID(session.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		if err := a.store.RemoveSession(cookie.Value); err != nil {
			return nil, err
		}
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
		return nil, nil
	}
	ttl, err := a.sessionTTL()
	if err != nil {
		return nil, err
	}
	if _, err := a.store.TouchSession(cookie.Value, ttl); err != nil {
		return nil, err
	}
	a.writeSessionCookie(w, r, cookie.Value, ttl)
	return user, nil
}

func withUser(r *http.Request, user *UserRecord) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), userContextKey, user))
}

func currentUser(r *http.Request) *UserRecord {
	user, _ := r.Context().Value(userContextKey).(*UserRecord)
	return user
}

func writeLoginGuardError(w http.ResponseWriter, err error) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	retryAfter := "300"
	message := "登录尝试过多，请稍后再试"
	if errors.Is(err, errPasswordCheckBusy) {
		retryAfter = "1"
		message = "登录服务繁忙，请稍后再试"
	}
	w.Header().Set("retry-after", retryAfter)
	writeError(w, http.StatusTooManyRequests, message)
}

func (a *App) attachUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := a.resolveUser(w, r)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "数据存储不可用")
			return
		}
		next.ServeHTTP(w, withUser(r, user))
	})
}

func requireAuth(next func(http.ResponseWriter, *http.Request, *UserRecord)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := currentUser(r)
		if user == nil {
			writeError(w, http.StatusUnauthorized, "未登录")
			return
		}
		next(w, r, user)
	}
}

func requireAdmin(next func(http.ResponseWriter, *http.Request, *UserRecord)) http.HandlerFunc {
	return requireAuth(func(w http.ResponseWriter, r *http.Request, user *UserRecord) {
		if user.Role != RoleAdmin {
			writeError(w, http.StatusForbidden, "需要管理员权限")
			return
		}
		next(w, r, user)
	})
}
