package executor

import (
	"bytes"
	"context"
	"github.com/MoeForge/moego/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	internalsignature "github.com/router-for-me/CLIProxyAPI/v8/internal/signature"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	"google.golang.org/protobuf/encoding/protowire"
)

// testVertexBareTinkSignature mirrors the Vertex AI serve-path shape: a TINK
// prefix byte, a four-byte key id and filler ciphertext, with no protobuf
// wrapper. Synthetic only; no provider payload is embedded.
func testVertexBareTinkSignature() string {
	payload := []byte{0x01, 0x8f, 0x3d, 0x6b, 0x5f}
	for len(payload) < 405 {
		payload = append(payload, byte(0xA0+len(payload)%0x20))
	}
	return base64.StdEncoding.EncodeToString(payload)
}

func testForeignProtobufField2Signature(keyID [4]byte) string {
	tink := append([]byte{0x01}, keyID[:]...)
	for len(tink) < 120 {
		tink = append(tink, byte(0x50+len(tink)%0x20))
	}
	var inner []byte
	inner = protowire.AppendTag(inner, 1, protowire.BytesType)
	inner = protowire.AppendBytes(inner, tink)
	var outer []byte
	outer = protowire.AppendTag(outer, 2, protowire.BytesType)
	outer = protowire.AppendBytes(outer, inner)
	return base64.StdEncoding.EncodeToString(outer)
}

func vertexBareTinkGeminiHistory(sig string) []byte {
	return []byte(`{"contents":[` +
		`{"role":"user","parts":[{"text":"list"}]},` +
		`{"role":"model","parts":[{"functionCall":{"name":"list_dir","args":{"path":"."}},"thoughtSignature":"` + sig + `"}]},` +
		`{"role":"user","parts":[{"functionResponse":{"name":"list_dir","response":{"files":[]}}}]}],` +
		`"tools":[{"functionDeclarations":[{"name":"list_dir","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}]}]}`)
}

const vertexBareTinkTestOKResponse = `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`

func TestGeminiExecutorReplacesVertexBareTinkSignature(t *testing.T) {
	sig := testVertexBareTinkSignature()
	var upstreamBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		upstreamBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(vertexBareTinkTestOKResponse))
	}))
	defer server.Close()

	exec := NewGeminiExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "test-key", "base_url": server.URL}}
	req := cliproxyexecutor.Request{Model: "gemini-3.1-pro-preview", Payload: vertexBareTinkGeminiHistory(sig)}
	if _, err := exec.Execute(context.Background(), auth, req, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatGemini}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	contents := ojson.GetBytes(upstreamBody, "contents").Array()
	found := false
	for _, content := range contents {
		for _, part := range content.Get("parts").Array() {
			if !part.Get("functionCall").Exists() {
				continue
			}
			found = true
			if got := part.Get("thoughtSignature").String(); got != internalsignature.GeminiSkipThoughtSignatureValidator {
				t.Fatalf("functionCall signature = %q, want bypass sentinel; body=%s", got, upstreamBody)
			}
		}
	}
	if !found {
		t.Fatalf("functionCall missing from upstream body: %s", upstreamBody)
	}
}

func TestAntigravityExecutorReplacesVertexBareTinkSignature(t *testing.T) {
	sig := testVertexBareTinkSignature()
	var upstreamBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		upstreamBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"response":` + vertexBareTinkTestOKResponse + `}`))
	}))
	defer server.Close()

	exec := NewAntigravityExecutor(&config.Config{RequestRetry: 1})
	auth := testAntigravityAuth(server.URL)
	auth.Metadata["project_id"] = "project-1"
	req := cliproxyexecutor.Request{Model: "gemini-3.6-flash-high", Payload: vertexBareTinkGeminiHistory(sig)}
	_, errExec := exec.Execute(context.Background(), auth, req, cliproxyexecutor.Options{
		SourceFormat:    sdktranslator.FormatGemini,
		ResponseFormat:  sdktranslator.FormatGemini,
		OriginalRequest: req.Payload,
	})
	if len(upstreamBody) == 0 {
		t.Fatalf("upstream body was not captured; Execute error = %v", errExec)
	}
	if bytes.Contains(upstreamBody, []byte(sig)) {
		t.Fatalf("Vertex bare-Tink signature leaked to Antigravity upstream: %s", upstreamBody)
	}
	if !bytes.Contains(upstreamBody, []byte(internalsignature.GeminiSkipThoughtSignatureValidator)) {
		t.Fatalf("bypass sentinel missing from Antigravity upstream: %s", upstreamBody)
	}
}

func TestGeminiVertexSanitizePreservesBareTinkSignature(t *testing.T) {
	sig := testVertexBareTinkSignature()
	out := internalsignature.SanitizeVertexGeminiRequestThoughtSignatures(vertexBareTinkGeminiHistory(sig), "contents")
	if got := ojson.GetBytes(out, "contents.1.parts.0.thoughtSignature").String(); got != sig {
		t.Fatalf("Vertex path signature = %q, want original bare-Tink value", got)
	}
}

func TestGeminiVertexExecutorReplacesForeignProtobufField2AndPreservesVertexIssued(t *testing.T) {
	foreignSig := testForeignProtobufField2Signature([4]byte{0x69, 0x14, 0x7d, 0x13})
	vertexIssuedSig := testForeignProtobufField2Signature([4]byte{0xaa, 0xbb, 0xcc, 0xdd})

	var upstreamBodies [][]byte
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		upstreamBodies = append(upstreamBodies, body)
		w.Header().Set("Content-Type", "application/json")
		callCount++
		if callCount == 1 {
			_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"list_dir","args":{"path":"."}},"thoughtSignature":"` + vertexIssuedSig + `"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`))
			return
		}
		_, _ = w.Write([]byte(vertexBareTinkTestOKResponse))
	}))
	defer server.Close()

	exec := NewGeminiVertexExecutor(&config.Config{})
	auth := &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "vertex-test-key", "base_url": server.URL}}

	req1 := cliproxyexecutor.Request{Model: "gemini-4-hydrogen", Payload: vertexBareTinkGeminiHistory(foreignSig)}
	if _, err := exec.Execute(context.Background(), auth, req1, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatGemini}); err != nil {
		t.Fatalf("Execute(turn 1) error = %v", err)
	}
	if got := ojson.GetBytes(upstreamBodies[0], "contents.1.parts.0.thoughtSignature").String(); got != internalsignature.GeminiSkipThoughtSignatureValidator {
		t.Fatalf("turn 1 foreign signature = %q, want %q; body=%s", got, internalsignature.GeminiSkipThoughtSignatureValidator, upstreamBodies[0])
	}

	req2 := cliproxyexecutor.Request{Model: "gemini-4-hydrogen", Payload: vertexBareTinkGeminiHistory(vertexIssuedSig)}
	if _, err := exec.Execute(context.Background(), auth, req2, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatGemini}); err != nil {
		t.Fatalf("Execute(turn 2) error = %v", err)
	}
	if got := ojson.GetBytes(upstreamBodies[1], "contents.1.parts.0.thoughtSignature").String(); got != vertexIssuedSig {
		t.Fatalf("turn 2 Vertex-issued signature = %q, want %q", got, vertexIssuedSig)
	}
}
