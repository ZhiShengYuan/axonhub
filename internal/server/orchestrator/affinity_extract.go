package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/llm/httpclient"
)

// maxAffinityValueBytes is the maximum allowed length for a raw affinity
// value. Values longer than this are ignored to bound memory usage and
// prevent accidental key inflation from accidental header/body bloat.
const maxAffinityValueBytes = 4096

// unknownModelScope is the model scope reported when the request body does
// not contain a parseable "model" field. Keeping a stable placeholder lets
// affinity entries group cross-model traffic on the same affinity key
// only when callers explicitly opt in.
const unknownModelScope = "unknown"

// affinityExactHeaders is the strict first-match-wins list of header names
// consulted before any regex or body fallback. Order is significant:
// the first non-empty, non-denylisted header wins.
var affinityExactHeaders = []string{
	"X-Session-Affinity",
	"X-Claude-Code-Session-Id",
	"Session_id",
	"Session-Id",
	"X-Litellm-Session-Id",
	"X-Amp-Thread-Id",
	"X-Session-Id",
	"X-Openai-Session-Id",
	"X-Task-ID",
}

// affinityHeaderDenylist contains header names that must never be used as
// affinity sources, even when they happen to match one of the regex
// patterns below. These are unstable, identity, transport, or
// provider-internal headers whose values do not represent a stable
// caller-controlled session.
var affinityHeaderDenylist = map[string]struct{}{
	"X-Request-Id":                  {},
	"X-Correlation-Id":              {},
	"Traceparent":                   {},
	"X-Trace-Id":                    {},
	"X-Litellm-Trace-Id":            {},
	"X-Client-Request-Id":           {},
	"Idempotency-Key":               {},
	"Authorization":                 {},
	"Cookie":                        {},
	"User-Agent":                    {},
	"X-Opencode-Request":            {},
	"X-Opencode-Project":            {},
	"X-Opencode-Client":             {},
	"X-Claude-Code-Agent-Id":        {},
	"X-Claude-Code-Parent-Agent-Id": {},
	"Openai-Organization":           {},
	"Openai-Project":                {},
	"Openai-Beta":                   {},
	"Anthropic-Version":             {},
	"Anthropic-Beta":                {},
	"Mcp-Session-Id":                {},
}

// affinityRegexPatterns is the ordered list of regex patterns applied to
// the remaining headers (those not in affinityExactHeaders and not in the
// denylist). Patterns are case-insensitive. The first non-empty, trimmed
// match wins.
var affinityRegexPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^x-.+-session-id$`),
	regexp.MustCompile(`(?i)^x-.+-thread-id$`),
	regexp.MustCompile(`(?i)^x-.+-conversation-id$`),
}

// sessionUserIDPattern matches values of the form "_session_<uuid>".
// Only these specific values are eligible as an affinity source from the
// metadata.user_id body field. The UUID portion matches the canonical
// 8-4-4-4-12 hex format used by Claude Code and similar clients.
var sessionUserIDPattern = regexp.MustCompile(`^_session_[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ExtractAffinity derives an AffinityState from the inbound HTTP request
// using a strict first-match-wins precedence:
//
//  1. Exact header names in affinityExactHeaders (case-insensitive).
//  2. Regex patterns in affinityRegexPatterns over the remaining headers,
//     skipping any header listed in affinityHeaderDenylist.
//  3. Body field fallbacks (metadata.user_id, metadata.session_id,
//     prompt_cache_key, conversation_id) — only when no header matched.
//
// If no affinity signal is found, ExtractAffinity returns (nil, nil) — a
// missing affinity is not an error.
//
// The returned AffinityState.Hash is the SHA256 of the raw affinity value;
// raw values are never stored or logged.
func ExtractAffinity(ctx context.Context, request *httpclient.Request) (*contexts.AffinityState, error) {
	if request == nil {
		return nil, nil
	}

	raw, source, found := extractFromHeaders(request.Headers)
	if !found && len(request.Body) > 0 {
		raw, source, found = extractFromBody(request.Body)
	}

	modelScope := extractModelScope(request.Body)

	if !found {
		return nil, nil
	}

	sum := sha256.Sum256([]byte(raw))
	hash := hex.EncodeToString(sum[:])

	return &contexts.AffinityState{
		Hash:       hash,
		Source:     source,
		ModelScope: modelScope,
	}, nil
}

// extractFromHeaders walks the request headers using the strict
// first-match-wins precedence. It returns the raw affinity value, the
// header name that produced it, and whether a match was found.
func extractFromHeaders(headers http.Header) (string, string, bool) {
	if len(headers) == 0 {
		return "", "", false
	}

	// Step 1: exact header names, in declared order.
	for _, name := range affinityExactHeaders {
		value := cleanHeaderValue(headers.Get(name))
		if value == "" {
			continue
		}

		return value, name, true
	}

	// Step 2: regex patterns over remaining headers, in declared order.
	// For each pattern, iterate remaining headers and pick the first one
	// whose value is non-empty and trimmed.
	for _, pat := range affinityRegexPatterns {
		for name, vals := range headers {
			if isDenylisted(name) {
				continue
			}

			// Skip headers already consumed in step 1.
			if isExactHeader(name) {
				continue
			}

			if !pat.MatchString(name) {
				continue
			}

			value := cleanHeaderValue(strings.Join(vals, ","))
			if value == "" {
				continue
			}

			return value, name, true
		}
	}

	return "", "", false
}

// extractFromBody parses the request body as JSON and walks the
// documented body field fallbacks in order. The first non-empty value
// wins.
func extractFromBody(body []byte) (string, string, bool) {
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", "", false
	}

	// metadata.user_id (only when it matches the _session_<uuid> pattern)
	if meta, ok := parsed["metadata"].(map[string]any); ok {
		if userID, ok := meta["user_id"].(string); ok {
			if sessionUserIDPattern.MatchString(userID) {
				return userID, "metadata.user_id", true
			}
		}

		// metadata.session_id
		if sessionID, ok := meta["session_id"].(string); ok {
			sessionID = strings.TrimSpace(sessionID)
			if sessionID != "" && len(sessionID) <= maxAffinityValueBytes {
				return sessionID, "metadata.session_id", true
			}
		}
	}

	// prompt_cache_key (top-level)
	if v, ok := parsed["prompt_cache_key"].(string); ok {
		v = strings.TrimSpace(v)
		if v != "" && len(v) <= maxAffinityValueBytes {
			return v, "prompt_cache_key", true
		}
	}

	// conversation_id (top-level)
	if v, ok := parsed["conversation_id"].(string); ok {
		v = strings.TrimSpace(v)
		if v != "" && len(v) <= maxAffinityValueBytes {
			return v, "conversation_id", true
		}
	}

	return "", "", false
}

// extractModelScope returns the model name from the request body JSON,
// or unknownModelScope when the field is absent or unparseable.
func extractModelScope(body []byte) string {
	if len(body) == 0 {
		return unknownModelScope
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return unknownModelScope
	}

	if v, ok := parsed["model"].(string); ok {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}

	return unknownModelScope
}

// cleanHeaderValue returns a trimmed header value, or "" when the value
// is empty, whitespace-only, or longer than maxAffinityValueBytes.
func cleanHeaderValue(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}

	if len(v) > maxAffinityValueBytes {
		return ""
	}

	return v
}

// isDenylisted reports whether the given header name is in the affinity
// header denylist. Comparison is case-insensitive.
func isDenylisted(name string) bool {
	_, ok := affinityHeaderDenylist[http.CanonicalHeaderKey(name)]
	return ok
}

// isExactHeader reports whether the given header name appears in
// affinityExactHeaders. Comparison is case-insensitive via http.Header.Get
// semantics, but we canonicalize both sides for robustness.
func isExactHeader(name string) bool {
	canon := http.CanonicalHeaderKey(name)
	for _, h := range affinityExactHeaders {
		if http.CanonicalHeaderKey(h) == canon {
			return true
		}
	}

	return false
}
