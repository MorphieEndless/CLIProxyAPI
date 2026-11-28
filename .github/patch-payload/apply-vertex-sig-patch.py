#!/usr/bin/env python3
"""Idempotently wire SanitizeVertexGeminiRequestThoughtSignaturesDoc & Vertex response provenance into gemini_vertex_executor.go."""
import pathlib
import sys

TARGET = pathlib.Path("internal/runtime/executor/gemini_vertex_executor.go")
text = TARGET.read_text(encoding="utf-8")

if "SanitizeVertexGeminiRequestThoughtSignaturesDoc" in text:
    print("gemini_vertex_executor.go is already patched.")
    sys.exit(0)

old_sanitize = 'internalsignature.SanitizeGeminiRequestThoughtSignaturesDoc(doc, "contents")'
new_sanitize = 'internalsignature.SanitizeVertexGeminiRequestThoughtSignaturesDoc(doc, "contents")'
assert text.count(old_sanitize) == 6, f"Expected 6 sanitize calls, found {text.count(old_sanitize)}"
text = text.replace(old_sanitize, new_sanitize)

old_replay_resp = """\tif !isImagenModel(baseModel) && antigravityUsesReasoningReplayCache(baseModel) {
\t\tvar errReplay error
\t\treplayScope, replayRequestPayload, _, errReplay = prepareGeminiTopLevelReasoningReplayPayloadDoc(ctx, baseModel, req, opts, doc)
\t\tif errReplay != nil {
\t\t\treturn resp, errReplay
\t\t}
\t}"""
new_replay_resp = """\tif !isImagenModel(baseModel) && antigravityUsesReasoningReplayCache(baseModel) {
\t\tvar changedReplay bool
\t\tvar errReplay error
\t\treplayScope, replayRequestPayload, changedReplay, errReplay = prepareGeminiTopLevelReasoningReplayPayloadDoc(ctx, baseModel, req, opts, doc)
\t\tif errReplay != nil {
\t\t\treturn resp, errReplay
\t\t}
\t\tif changedReplay {
\t\t\tinternalsignature.SanitizeVertexGeminiRequestThoughtSignaturesDoc(doc, "contents")
\t\t}
\t}"""
assert text.count(old_replay_resp) == 2, f"Expected 2 non-stream replay blocks, found {text.count(old_replay_resp)}"
text = text.replace(old_replay_resp, new_replay_resp)

old_replay_stream = """\tif !isImagenModel(baseModel) && antigravityUsesReasoningReplayCache(baseModel) {
\t\tvar errReplay error
\t\treplayScope, replayRequestPayload, _, errReplay = prepareGeminiTopLevelReasoningReplayPayloadDoc(ctx, baseModel, req, opts, doc)
\t\tif errReplay != nil {
\t\t\treturn nil, errReplay
\t\t}
\t}"""
new_replay_stream = """\tif !isImagenModel(baseModel) && antigravityUsesReasoningReplayCache(baseModel) {
\t\tvar changedReplay bool
\t\tvar errReplay error
\t\treplayScope, replayRequestPayload, changedReplay, errReplay = prepareGeminiTopLevelReasoningReplayPayloadDoc(ctx, baseModel, req, opts, doc)
\t\tif errReplay != nil {
\t\t\treturn nil, errReplay
\t\t}
\t\tif changedReplay {
\t\t\tinternalsignature.SanitizeVertexGeminiRequestThoughtSignaturesDoc(doc, "contents")
\t\t}
\t}"""
assert text.count(old_replay_stream) == 2, f"Expected 2 stream replay blocks, found {text.count(old_replay_stream)}"
text = text.replace(old_replay_stream, new_replay_stream)

old_replay_count = """\tif !isImagenModel(baseModel) && antigravityUsesReasoningReplayCache(baseModel) {
\t\tif _, errReplay := prepareGeminiTopLevelCountTokensReasoningReplayPayloadDoc(ctx, baseModel, req, opts, doc); errReplay != nil {
\t\t\treturn cliproxyexecutor.Response{}, errReplay
\t\t}
\t}"""
new_replay_count = """\tif !isImagenModel(baseModel) && antigravityUsesReasoningReplayCache(baseModel) {
\t\tif changedReplay, errReplay := prepareGeminiTopLevelCountTokensReasoningReplayPayloadDoc(ctx, baseModel, req, opts, doc); errReplay != nil {
\t\t\treturn cliproxyexecutor.Response{}, errReplay
\t\t} else if changedReplay {
\t\t\tinternalsignature.SanitizeVertexGeminiRequestThoughtSignaturesDoc(doc, "contents")
\t\t}
\t}"""
assert text.count(old_replay_count) == 2, f"Expected 2 countTokens replay blocks, found {text.count(old_replay_count)}"
text = text.replace(old_replay_count, new_replay_count)

old_cache_nonstream = "\t\tcacheAntigravityReasoningReplayFromResponse(ctx, replayScope, replayRequestPayload, data)"
new_cache_nonstream = "\t\tinternalsignature.RecordGeminiVertexThoughtSignaturesFromResponse(data)\n\t\tcacheAntigravityReasoningReplayFromResponse(ctx, replayScope, replayRequestPayload, data)"
assert text.count(old_cache_nonstream) == 2, f"Expected 2 non-stream response hooks, found {text.count(old_cache_nonstream)}"
text = text.replace(old_cache_nonstream, new_cache_nonstream)

old_stream_raw = "\t\t\trawPayload := helps.JSONPayload(line)"
new_stream_raw = "\t\t\trawPayload := helps.JSONPayload(line)\n\t\t\tinternalsignature.RecordGeminiVertexThoughtSignaturesFromResponse(rawPayload)"
assert text.count(old_stream_raw) == 2, f"Expected 2 stream response hooks, found {text.count(old_stream_raw)}"
text = text.replace(old_stream_raw, new_stream_raw)

TARGET.write_text(text, encoding="utf-8")
print("Patched gemini_vertex_executor.go successfully.")
