package router

import (
	"errors"
	"net/http"
	"strings"

	"github.com/dgrijalva/jwt-go"
	"github.com/reddec/git-pipe/internal"
	"go.uber.org/zap"
)

const (
	HeaderUser = "X-User"
)

var ErrSigningMethodUnsupported = errors.New("signing method unsupported")

// JWTClaims defined extended JWT claims schema.
type JWTClaims struct {
	jwt.StandardClaims
	Methods []string `json:"methods,omitempty"`
}

// JWT based authorization and authentication per-group (repo).
// subject is defining restriction for allowed group, methods for allowed HTTP methods.
// Sets HeaderUser to the request object in case of success.
func JWT(sharedKey string) RouteHandler {
	return RouteHandlerFunc(func(writer http.ResponseWriter, request *http.Request, route *Route) error {
		logger := internal.LoggerFromContext(request.Context()).Named("jwt")
		token := getToken(request)
		if token == "" {
			http.Error(writer, "token malformed", http.StatusUnauthorized)
			return ErrAbort
		}

		info, err := jwt.ParseWithClaims(token, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, ErrSigningMethodUnsupported
			}
			return []byte(sharedKey), nil
		})

		if err != nil {
			http.Error(writer, "", http.StatusUnauthorized)
			logger.Info("authorization failed", zap.Error(err))
			return ErrAbort
		}

		meta, ok := info.Claims.(*JWTClaims)
		if !ok {
			http.Error(writer, "", http.StatusUnauthorized)
			logger.Info("claims base validate failed")
			return ErrAbort
		}
		if meta.Audience == "" {
			http.Error(writer, "", http.StatusUnauthorized)
			logger.Info("audience not set")
			return ErrAbort
		}
		logger = logger.With(zap.String("audience", meta.Audience), zap.String("subject", meta.Subject), zap.Strings("methods", meta.Methods))
		if meta.Subject != "" && !strings.EqualFold(route.Service.Namespace, meta.Subject) {
			http.Error(writer, "", http.StatusForbidden)
			logger.Info("namespace not allowed for the subject")
			return ErrAbort
		}
		if len(meta.Methods) > 0 && !containsFold(meta.Methods, request.Method) {
			http.Error(writer, "", http.StatusForbidden)
			logger.Info("method not allowed")
			return ErrAbort
		}
		request.Header.Set(HeaderUser, meta.Audience)
		return nil
	})
}

func containsFold(list []string, value string) bool {
	for _, k := range list {
		if strings.EqualFold(k, value) {
			return true
		}
	}
	return false
}

//nolint:gomnd
func getToken(request *http.Request) string {
	token := request.Header.Get("Authorization")
	if token == "" {
		return request.URL.Query().Get("token")
	}
	parts := strings.SplitN(strings.TrimSpace(token), " ", 2)
	if len(parts) != 2 {
		return ""
	}
	kind := strings.TrimSpace(parts[0])
	token = strings.TrimSpace(parts[1])
	if !strings.EqualFold(kind, "bearer") {
		return ""
	}
	return token
}
