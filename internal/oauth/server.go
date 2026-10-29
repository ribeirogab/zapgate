package oauth

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/ribeirogab/zapgate/internal/audit"
	"github.com/ribeirogab/zapgate/internal/clock"
	"github.com/ribeirogab/zapgate/internal/store"
)

const (
	ProtectedResourcePath      = "/.well-known/oauth-protected-resource"
	AuthorizationServerPath    = "/.well-known/oauth-authorization-server"
	RegisterPath               = "/oauth/register"
	AuthorizePath              = "/oauth/authorize"
	TokenPath                  = "/oauth/token"
	RevokePath                 = "/oauth/revoke"
	ResourcePath               = "/mcp"
	Scope                      = "whatsapp"
	resourceName               = "zapgate"
	maxFormBytes               = 64 << 10
	corsAllowedHeaders         = "Authorization, Content-Type"
	corsAllowedMethods         = "GET, POST, OPTIONS"
	corsMaxAgeSeconds          = "86400"
	headerAllowOrigin          = "Access-Control-Allow-Origin"
	headerAllowHeaders         = "Access-Control-Allow-Headers"
	headerAllowMethods         = "Access-Control-Allow-Methods"
	headerMaxAge               = "Access-Control-Max-Age"
	anyOrigin                  = "*"
	contentTypeJSON            = "application/json"
	contentTypeHTML            = "text/html; charset=utf-8"
	cacheControlNoStore        = "no-store"
	headerCacheControl         = "Cache-Control"
	headerPragma               = "Pragma"
	pragmaNoCache              = "no-cache"
	headerContentType          = "Content-Type"
	authMethodNone             = "none"
	authMethodSecretPost       = "client_secret_post"
	authMethodSecretBasic      = "client_secret_basic"
	grantTypeAuthorizationCode = "authorization_code"
	grantTypeRefreshToken      = "refresh_token"
	responseTypeCode           = "code"
	challengeMethodS256        = "S256"
)

var supportedAuthMethods = []string{authMethodNone, authMethodSecretPost, authMethodSecretBasic}

type GrantEvents interface {
	GrantRevoked(grantID int64)
}

type Deps struct {
	Store         *store.Store
	Clock         clock.Clock
	Audit         *audit.Logger
	Grants        GrantEvents
	PublicURL     string
	OwnerPassword string
	Log           *slog.Logger
}

type Server struct {
	deps    Deps
	limiter *passwordLimiter
}

func NewServer(deps Deps) *Server {
	return &Server{deps: deps, limiter: newPasswordLimiter(deps.Clock)}
}

func (s *Server) Register(mux *http.ServeMux) {
	resourceMetadata := s.protectedResourceMetadata()
	mux.Handle(ProtectedResourcePath, resourceMetadata)
	mux.Handle(ProtectedResourcePath+ResourcePath, resourceMetadata)
	mux.Handle(AuthorizationServerPath, withCORS(http.HandlerFunc(s.authorizationServerMetadata)))
	mux.Handle(RegisterPath, withCORS(onlyPost(s.register)))
	mux.HandleFunc("GET "+AuthorizePath, s.authorizePage)
	mux.HandleFunc("POST "+AuthorizePath, s.authorizeDecision)
	mux.Handle(TokenPath, withCORS(onlyPost(s.token)))
	mux.Handle(RevokePath, withCORS(onlyPost(s.revoke)))
}

func (s *Server) issuer() string { return s.deps.PublicURL }

func (s *Server) resource() string { return s.deps.PublicURL + ResourcePath }

func (s *Server) endpoint(path string) string { return s.deps.PublicURL + path }

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(headerAllowOrigin, anyOrigin)
		if r.Method == http.MethodOptions {
			w.Header().Set(headerAllowHeaders, corsAllowedHeaders)
			w.Header().Set(headerAllowMethods, corsAllowedMethods)
			w.Header().Set(headerMaxAge, corsMaxAgeSeconds)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func onlyPost(fn http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST, OPTIONS")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		fn(w, r)
	})
}

func (s *Server) warn(ctx context.Context, msg string, err error) {
	s.deps.Log.WarnContext(ctx, msg, "error", err)
}
