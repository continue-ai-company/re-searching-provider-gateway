package cliproxy

import (
	"context"
	"strings"

	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func normalizeProviderAllowlist(providers []string) map[string]struct{} {
	if providers == nil {
		return nil
	}
	normalized := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		provider = strings.ToLower(strings.TrimSpace(provider))
		if provider == "" {
			continue
		}
		normalized[provider] = struct{}{}
	}
	return normalized
}

func (s *Service) providerAllowed(provider string) bool {
	if s == nil || !s.providerAllowlistEnabled {
		return true
	}
	_, ok := s.providerAllowlist[strings.ToLower(strings.TrimSpace(provider))]
	return ok
}

func (s *Service) pruneDisallowedAuths(ctx context.Context) {
	if s == nil || s.coreManager == nil || !s.providerAllowlistEnabled {
		return
	}
	for _, auth := range s.coreManager.List() {
		if auth == nil || s.providerAllowed(auth.Provider) {
			continue
		}
		s.removeDisallowedAuth(ctx, auth.ID)
	}
}

func (s *Service) removeDisallowedAuth(ctx context.Context, id string) {
	if s == nil || s.coreManager == nil {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	GlobalModelRegistry().UnregisterClient(id)
	s.coreManager.Remove(coreauth.WithSkipPersist(ctx), id)
}
