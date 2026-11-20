package signature

import (
	"bytes"
	"crypto/sha256"
	"strings"
	"sync"

	"github.com/MoeForge/moego/ojson"
)

const vertexIssuedSignatureGenerationCap = 4096

type vertexSignatureRegistry struct {
	mu       sync.RWMutex
	current  map[[32]byte]struct{}
	previous map[[32]byte]struct{}
}

var defaultVertexSignatureRegistry = &vertexSignatureRegistry{}

func (r *vertexSignatureRegistry) record(rawSignature string) {
	payload := SignaturePayloadWithoutProviderPrefix(rawSignature)
	if payload == "" || IsGeminiThoughtSignatureBypass(payload) {
		return
	}
	sum := sha256.Sum256([]byte(payload))

	r.mu.RLock()
	_, exists := r.current[sum]
	r.mu.RUnlock()
	if exists {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.current == nil {
		r.current = make(map[[32]byte]struct{}, 256)
	}
	if _, exists = r.current[sum]; exists {
		return
	}
	if len(r.current) >= vertexIssuedSignatureGenerationCap {
		r.previous = r.current
		r.current = make(map[[32]byte]struct{}, 256)
	}
	r.current[sum] = struct{}{}
}

func (r *vertexSignatureRegistry) contains(rawSignature string) bool {
	payload := SignaturePayloadWithoutProviderPrefix(rawSignature)
	if payload == "" {
		return false
	}
	sum := sha256.Sum256([]byte(payload))

	r.mu.RLock()
	_, inCurrent := r.current[sum]
	_, inPrevious := r.previous[sum]
	r.mu.RUnlock()
	return inCurrent || inPrevious
}

func (r *vertexSignatureRegistry) reset() {
	r.mu.Lock()
	r.current = nil
	r.previous = nil
	r.mu.Unlock()
}

// RecordGeminiVertexIssuedSignature records a thoughtSignature emitted by a
// Vertex AI Gemini endpoint on this CPA instance so non-bare-Tink Vertex
// signatures remain replayable within the local Vertex key domain.
func RecordGeminiVertexIssuedSignature(rawSignature string) {
	defaultVertexSignatureRegistry.record(rawSignature)
}

// IsRecordedGeminiVertexSignature reports whether rawSignature was previously
// observed in a Vertex AI response handled by this CPA instance.
func IsRecordedGeminiVertexSignature(rawSignature string) bool {
	return defaultVertexSignatureRegistry.contains(rawSignature)
}

// RecordGeminiVertexThoughtSignaturesFromResponse scans a Gemini/Vertex JSON
// response (or single SSE JSON chunk) and records any emitted thoughtSignature
// values into the Vertex provenance registry. Chunks without signature fields
// take a fast byte-substring exit with zero JSON parsing overhead.
func RecordGeminiVertexThoughtSignaturesFromResponse(payload []byte) {
	if len(payload) == 0 ||
		(!bytes.Contains(payload, []byte("thoughtSignature")) && !bytes.Contains(payload, []byte("thought_signature"))) {
		return
	}
	candidates := ojson.GetBytesNoCopy(payload, "candidates")
	if !candidates.IsArray() {
		candidates = ojson.GetBytesNoCopy(payload, "response.candidates")
	}
	if !candidates.IsArray() {
		return
	}
	candidates.ForEach(func(_, candidate ojson.Result) bool {
		parts := candidate.Get("content.parts")
		if !parts.IsArray() {
			return true
		}
		parts.ForEach(func(_, part ojson.Result) bool {
			if sig, ok := geminiPartThoughtSignature(part); ok && strings.TrimSpace(sig) != "" {
				RecordGeminiVertexIssuedSignature(sig)
			}
			return true
		})
		return true
	})
}

// IsCompatibleGeminiVertexSignature reports whether rawSignature can be safely
// forwarded to a Vertex AI Gemini endpoint without risking HTTP 400
// "Invalid thought signature." due to key-domain mismatch:
//  1. Gemini documented bypass sentinels are accepted (subject to first-call rules).
//  2. Vertex bare-Tink signatures (0x01 + Vertex Tink key ID) are natively Vertex-scoped.
//  3. Other valid Gemini envelopes (e.g. protobuf_field_2) are accepted only if
//     previously recorded from an outbound Vertex AI response on this instance.
func IsCompatibleGeminiVertexSignature(rawSignature string) bool {
	payload := SignaturePayloadWithoutProviderPrefix(rawSignature)
	if payload == "" {
		return false
	}
	if IsGeminiThoughtSignatureBypass(payload) {
		return true
	}
	if IsGeminiVertexBareTinkSignature(payload) {
		return true
	}
	return IsRecordedGeminiVertexSignature(payload)
}
