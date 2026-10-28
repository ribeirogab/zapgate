package oauth

import (
	"encoding/json"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

type authorizationServerMetadata struct {
	Issuer                                     string   `json:"issuer"`
	AuthorizationEndpoint                      string   `json:"authorization_endpoint"`
	TokenEndpoint                              string   `json:"token_endpoint"`
	RegistrationEndpoint                       string   `json:"registration_endpoint"`
	RevocationEndpoint                         string   `json:"revocation_endpoint"`
	ScopesSupported                            []string `json:"scopes_supported"`
	ResponseTypesSupported                     []string `json:"response_types_supported"`
	ResponseModesSupported                     []string `json:"response_modes_supported"`
	GrantTypesSupported                        []string `json:"grant_types_supported"`
	TokenEndpointAuthMethodsSupported          []string `json:"token_endpoint_auth_methods_supported"`
	RevocationEndpointAuthMethodsSupported     []string `json:"revocation_endpoint_auth_methods_supported"`
	CodeChallengeMethodsSupported              []string `json:"code_challenge_methods_supported"`
	AuthorizationResponseIssParameterSupported bool     `json:"authorization_response_iss_parameter_supported"`
}

func (s *Server) protectedResourceMetadata() http.Handler {
	return auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource:               s.resource(),
		AuthorizationServers:   []string{s.issuer()},
		ScopesSupported:        []string{Scope},
		BearerMethodsSupported: []string{"header"},
		ResourceName:           resourceName,
	})
}

func (s *Server) authorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, authorizationServerMetadata{
		Issuer:                                     s.issuer(),
		AuthorizationEndpoint:                      s.endpoint(AuthorizePath),
		TokenEndpoint:                              s.endpoint(TokenPath),
		RegistrationEndpoint:                       s.endpoint(RegisterPath),
		RevocationEndpoint:                         s.endpoint(RevokePath),
		ScopesSupported:                            []string{Scope},
		ResponseTypesSupported:                     []string{responseTypeCode},
		ResponseModesSupported:                     []string{"query"},
		GrantTypesSupported:                        []string{grantTypeAuthorizationCode, grantTypeRefreshToken},
		TokenEndpointAuthMethodsSupported:          supportedAuthMethods,
		RevocationEndpointAuthMethodsSupported:     supportedAuthMethods,
		CodeChallengeMethodsSupported:              []string{challengeMethodS256},
		AuthorizationResponseIssParameterSupported: true,
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set(headerContentType, contentTypeJSON)
	w.Header().Set(headerCacheControl, cacheControlNoStore)
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}
