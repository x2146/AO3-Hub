package app

import (
	"context"
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
	if len(parts) != 6 || parts[1] != "argon2id" {
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
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false
	}
	expected, err := b64.DecodeString(parts[5])
	if err != nil {
		return false
	}
	actual := argon2.IDKey([]byte(plain), salt, uint32(timeCost), uint32(memory), uint8(threads), uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func (a *App) sessionTTL() time.Duration {
	cfg, err := a.store.LoadConfig()
	if err != nil || cfg.Auth.SessionTTLDays <= 0 {
		return 30 * 24 * time.Hour
	}
	return time.Duration(cfg.Auth.SessionTTLDays) * 24 * time.Hour
}

func (a *App) startSession(w http.ResponseWriter, r *http.Request, userID string) error {
	ttl := a.sessionTTL()
	session, err := a.store.CreateSession(userID, ttl)
	if err != nil {
		return err
	}
	a.writeSessionCookie(w, r, session.Token, ttl)
	return nil
}

func (a *App) endSession(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(cookieName); err == nil {
		a.store.RemoveSession(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *App) writeSessionCookie(w http.ResponseWriter, r *http.Request, token string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
	})
}

func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	xf := strings.Split(r.Header.Get("x-forwarded-proto"), ",")[0]
	return strings.TrimSpace(xf) == "https"
}

func (a *App) resolveUser(w http.ResponseWriter, r *http.Request) *UserRecord {
	cookie, err := r.Cookie(cookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}
	session := a.store.FindValidSession(cookie.Value)
	if session == nil {
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
		return nil
	}
	user := a.store.FindUserByID(session.UserID)
	if user == nil {
		a.store.RemoveSession(cookie.Value)
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
		return nil
	}
	ttl := a.sessionTTL()
	a.store.TouchSession(cookie.Value, ttl)
	a.writeSessionCookie(w, r, cookie.Value, ttl)
	return user
}

func withUser(r *http.Request, user *UserRecord) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), userContextKey, user))
}

func currentUser(r *http.Request) *UserRecord {
	user, _ := r.Context().Value(userContextKey).(*UserRecord)
	return user
}

func (a *App) attachUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := a.resolveUser(w, r)
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
