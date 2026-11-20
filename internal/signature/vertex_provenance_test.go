package signature

import (
	"fmt"
	"testing"

	"github.com/MoeForge/moego/base64"
	"github.com/MoeForge/moego/ojson"
)

func testVertexBareTinkSigForSanitize() string {
	payload := []byte{0x01, 0x8f, 0x3d, 0x6b, 0x5f}
	for len(payload) < 128 {
		payload = append(payload, byte(0x40+len(payload)%0x20))
	}
	return base64.StdEncoding.EncodeToString(payload)
}

func TestSanitizeVertexGeminiReplacesForeignProtobufField2AndPreservesBareTink(t *testing.T) {
	defaultVertexSignatureRegistry.reset()
	t.Cleanup(defaultVertexSignatureRegistry.reset)

	bareSig := testVertexBareTinkSigForSanitize()
	foreignSig := testGemini3ThoughtSignature([]byte{0x01, 0x69, 0x14, 0x7d, 0x13, 0xaa, 0xbb, 0xcc})

	input := []byte(`{"contents":[` +
		`{"role":"user","parts":[{"text":"hi"}]},` +
		`{"role":"model","parts":[{"thought":true,"text":"t1"},{"functionCall":{"name":"read_a","args":{}},"thoughtSignature":"` + bareSig + `"}]},` +
		`{"role":"user","parts":[{"functionResponse":{"name":"read_a","response":{"ok":true}}}]},` +
		`{"role":"model","parts":[{"thought":true,"text":"t2"},{"functionCall":{"name":"read_b","args":{}},"thoughtSignature":"` + foreignSig + `"},{"functionCall":{"name":"read_c","args":{}},"thoughtSignature":"` + foreignSig + `"},{"text":"after","thoughtSignature":"` + foreignSig + `"}]},` +
		`{"role":"user","parts":[{"functionResponse":{"name":"read_b","response":{"ok":true}}},{"functionResponse":{"name":"read_c","response":{"ok":true}}}]}]}`)

	out := SanitizeVertexGeminiRequestThoughtSignatures(input, "contents")

	if got := ojson.GetBytes(out, "contents.1.parts.1.thoughtSignature").String(); got != bareSig {
		t.Fatalf("Vertex bare-Tink signature modified: got %q, want %q", got, bareSig)
	}
	if got := ojson.GetBytes(out, "contents.3.parts.1.thoughtSignature").String(); got != GeminiSkipThoughtSignatureValidator {
		t.Fatalf("foreign protobuf_field_2 first functionCall = %q, want %q; out=%s", got, GeminiSkipThoughtSignatureValidator, out)
	}
	if ojson.GetBytes(out, "contents.3.parts.2.thoughtSignature").Exists() {
		t.Fatalf("foreign signature on sibling functionCall should be dropped; out=%s", out)
	}
	if ojson.GetBytes(out, "contents.3.parts.3.thoughtSignature").Exists() {
		t.Fatalf("foreign signature on text part should be dropped; out=%s", out)
	}
}

func TestSanitizeVertexGeminiPreservesRecordedVertexProtobufField2(t *testing.T) {
	defaultVertexSignatureRegistry.reset()
	t.Cleanup(defaultVertexSignatureRegistry.reset)

	vertexField2Sig := testGemini3ThoughtSignature([]byte{0x01, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77})
	respJSON := []byte(`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"tool_x","args":{}},"thoughtSignature":"` + vertexField2Sig + `"}]}}]}`)
	RecordGeminiVertexThoughtSignaturesFromResponse(respJSON)

	input := []byte(`{"contents":[{"role":"model","parts":[{"functionCall":{"name":"tool_x","args":{}},"thoughtSignature":"` + vertexField2Sig + `"}]}]}`)
	out := SanitizeVertexGeminiRequestThoughtSignatures(input, "contents")

	if got := ojson.GetBytes(out, "contents.0.parts.0.thoughtSignature").String(); got != vertexField2Sig {
		t.Fatalf("recorded Vertex signature was stripped: got %q, want %q", got, vertexField2Sig)
	}
	if &out[0] != &input[0] {
		t.Fatal("expected zero-copy pass-through for recorded canonical Vertex signature")
	}
}

func TestVertexSignatureRegistryGenerationRollover(t *testing.T) {
	defaultVertexSignatureRegistry.reset()
	t.Cleanup(defaultVertexSignatureRegistry.reset)

	firstSig := testGemini3ThoughtSignature([]byte{0x01, 0x00, 0x00, 0x00, 0x01})
	RecordGeminiVertexIssuedSignature(firstSig)

	for i := 0; i < vertexIssuedSignatureGenerationCap; i++ {
		RecordGeminiVertexIssuedSignature(fmt.Sprintf("sig-gen1-%d", i))
	}
	if !IsRecordedGeminiVertexSignature(firstSig) {
		t.Fatal("expected signature in previous generation to remain recognized")
	}

	for i := 0; i < vertexIssuedSignatureGenerationCap; i++ {
		RecordGeminiVertexIssuedSignature(fmt.Sprintf("sig-gen2-%d", i))
	}
	if IsRecordedGeminiVertexSignature(firstSig) {
		t.Fatal("expected signature older than two generations to be evicted")
	}
}
