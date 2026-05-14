package orchestrator

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/looplj/axonhub/internal/log"
)

var (
	// affinityLRUSize is the default LRU cache size for affinity mappings.
	affinityLRUSize = 1024

	// affinityTTL is the sliding TTL for affinity cache entries.
	affinityTTL = 24 * time.Hour
)

// affinityScope holds the components used to build an HMAC affinity key.
// All fields are required for determinism; zero values produce distinct keys.
type affinityScope struct {
	ProjectID         int
	APIKeyID          int
	OriginalModel     string
	ResolvedProvider   string
	SessionAffinity   string
}

// affinityCacheEntry holds the cached channel selection with timestamp.
type affinityCacheEntry struct {
	ChannelID  int
	UpdatedAt  time.Time
}

// String serializes the affinity scope to a pipe-delimited string for HMAC input.
// No raw affinity values are logged; callers must ensure sanitization.
func (s affinityScope) String() string {
	return fmt.Sprintf("%d|%d|%s|%s|%s",
		s.ProjectID, s.APIKeyID, s.OriginalModel, s.ResolvedProvider, s.SessionAffinity)
}

// SessionAffinityService provides HMAC-based affinity key generation and
// bounded last-success caching for channel selection stability.
type SessionAffinityService struct {
	secret   []byte
	cache    *lru.Cache[string, affinityCacheEntry]
	mu       sync.RWMutex
	generates bool // true if using a process-local random secret
}

// NewSessionAffinityService creates a SessionAffinityService.
// If a secret is provided (e.g., from app config), it is used directly.
// Otherwise, a process-local random 32-byte secret is generated at startup;
// mappings will reset on process restart and this is documented in notepad.
func NewSessionAffinityService(secret []byte) *SessionAffinityService {
	svc := &SessionAffinityService{
		cache: nil,
	}

	if len(secret) > 0 {
		svc.secret = secret
	} else {
		// Generate a process-local random secret; documented as restart-reset.
		generated := make([]byte, 32)
		_, _ = rand.Read(generated)
		svc.secret = generated
		svc.generates = true
	}

	cache, _ := lru.New[string, affinityCacheEntry](affinityLRUSize)
	svc.cache = cache

	return svc
}

// GeneratesSecret returns true if the service generated its own process-local secret
// (meaning mappings reset on restart).
func (s *SessionAffinityService) GeneratesSecret() bool {
	return s.generates
}

// BuildKey computes an HMAC-SHA256 key from the given affinity scope.
// The returned hex string is safe to use as a cache key and for logging
// (contains no raw sensitive input).
func (s *SessionAffinityService) BuildKey(scope affinityScope) string {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(scope.String()))
	return hex.EncodeToString(mac.Sum(nil))
}

// Get returns the cached channel ID and whether it exists and is non-expired.
func (s *SessionAffinityService) Get(scope affinityScope) (channelID int, ok bool) {
	key := s.BuildKey(scope)

	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, exists := s.cache.Get(key)
	if !exists {
		return 0, false
	}

	// Sliding TTL: entry is valid if within affinityTTL window.
	if time.Since(entry.UpdatedAt) > affinityTTL {
		return 0, false
	}

	return entry.ChannelID, true
}

// Set records a successful channel selection for the given scope.
// Implements sliding TTL: updates the timestamp, resetting the window.
func (s *SessionAffinityService) Set(ctx context.Context, scope affinityScope, channelID int) {
	key := s.BuildKey(scope)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.cache.Add(key, affinityCacheEntry{
		ChannelID: channelID,
		UpdatedAt:  time.Now(),
	})

	if log.DebugEnabled(ctx) {
		log.Debug(ctx, "Session affinity cached",
			log.String("affinity_key", safeAffinityKeyPrefix(key)),
		)
	}
}

// safeAffinityKeyPrefix returns a truncated, safe prefix of an HMAC key for logging.
// Never logs raw session affinity values.
func safeAffinityKeyPrefix(key string) string {
	if len(key) >= 16 {
		return key[:16] + "..."
	}
	return key
}

// BuildAffinityScope builds an affinityScope from the given parameters.
// The resolvedProvider should come from the channel that will handle the request.
func BuildAffinityScope(projectID int, apiKeyID int, originalModel, resolvedProvider, sessionAffinity string) affinityScope {
	return affinityScope{
		ProjectID:        projectID,
		APIKeyID:        apiKeyID,
		OriginalModel:   originalModel,
		ResolvedProvider: resolvedProvider,
		SessionAffinity: sessionAffinity,
	}
}

// ApplyAffinityPreference reorders candidates to prefer the affinity-mapped channel if eligible.
// Returns the reordered candidates, the preferred channel ID (0 if none), and whether affinity was applied.
// This should be called after candidates are selected and load-balanced.
func ApplyAffinityPreference(candidates []*ChannelModelsCandidate, service *SessionAffinityService, scope affinityScope) ([]*ChannelModelsCandidate, int, bool) {
	if service == nil || scope.SessionAffinity == "" {
		return candidates, 0, false
	}

	if len(candidates) == 0 {
		return candidates, 0, false
	}

	mappedChannelID, ok := service.Get(scope)
	if !ok {
		return candidates, 0, false
	}

	for i, c := range candidates {
		if c.Channel.ID == mappedChannelID {
			if i == 0 {
				return candidates, mappedChannelID, true
			}
			newCandidates := make([]*ChannelModelsCandidate, 0, len(candidates))
			newCandidates = append(newCandidates, c)
			for j, cc := range candidates {
				if j != i {
					newCandidates = append(newCandidates, cc)
				}
			}
			return newCandidates, mappedChannelID, true
		}
	}

	return candidates, 0, false
}

// UpdateAffinityOnSuccess updates the affinity mapping if the request succeeded with a different channel than preferred.
// This should be called when a request succeeds.
// If fallbackChannelID is different from preferredChannelID, update affinity to point to fallbackChannelID.
// If fallbackChannelID equals preferredChannelID or preferredChannelID is 0, no update is needed.
func UpdateAffinityOnSuccess(ctx context.Context, service *SessionAffinityService, scope affinityScope, fallbackChannelID, preferredChannelID int) {
	if service == nil || scope.SessionAffinity == "" {
		return
	}

	if fallbackChannelID == 0 {
		return
	}

	if fallbackChannelID == preferredChannelID {
		return
	}

	if log.DebugEnabled(ctx) {
		log.Debug(ctx, "Updating affinity mapping after successful fallback",
			log.String("affinity_key", safeAffinityKeyPrefix(service.BuildKey(scope))),
			log.Int("old_preferred_channel", preferredChannelID),
			log.Int("new_channel", fallbackChannelID),
		)
	}

	service.Set(ctx, scope, fallbackChannelID)
}