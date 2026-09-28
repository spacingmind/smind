package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
)

// requestBodyParseCap bounds how much of an incoming request body proxy.go
// buffers in order to read "model"/"stream" (and, for an OpenAI stream
// lacking it, to inject stream_options.include_usage). A body under this
// cap is fully buffered and re-forwarded from memory; a body over it is
// streamed straight through unmodified/unparsed (see readRequestBody),
// exactly like the pre-M1 code always did. Ordinary LLM requests (a
// system prompt plus a long conversation) comfortably fit; a request over
// this is rare enough that losing its model/stream/include_usage handling
// is an acceptable trade against buffering an unbounded body in memory.
const requestBodyParseCap = 5 << 20 // 5 MiB

// responseBodyParseCap bounds how much of a non-streaming response body
// this package buffers for usage extraction (M1's "Non-stream parsing is
// bounded; over the cap, tokens are NULL" requirement). The response is
// still streamed to the client in full regardless -- this only caps the
// side copy used for json.Unmarshal.
const responseBodyParseCap = 5 << 20 // 5 MiB

// sseLineCap bounds a single buffered-but-not-yet-newline-terminated SSE
// line during streaming usage extraction, so a pathological upstream that
// never sends a newline can't grow sseUsageScanner.pending without bound.
// Real usage-bearing SSE lines (message_start/message_delta/the final
// OpenAI usage chunk) are a few hundred bytes at most; a content-delta
// line this large would be unusual but is simply dropped from parsing,
// never from what reaches the client (see teeCaptureWriter).
const sseLineCap = 1 << 20 // 1 MiB

// tokenUsage holds whatever usage fields have been observed so far; a nil
// field means "not seen", matching store.RequestLog's own "nil means
// unknown" convention (see that type's doc comment) -- this is exactly
// what toRequestLogFields hands off to the row being built.
type tokenUsage struct {
	Input, Output, CacheRead, CacheWrite, Reasoning *int64
}

// requestMeta is the subset of an incoming request body proxy.go needs:
// Anthropic's Messages API and OpenAI's Chat Completions API both use
// these same two top-level field names, so one struct covers both
// providers.
type requestMeta struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

// parseRequestMeta best-effort decodes body's model/stream. body is
// assumed already known to be under requestBodyParseCap (proxy.go never
// calls this otherwise) -- a decode failure (body isn't even JSON) just
// means an unknown model/stream, not a proxy error.
func parseRequestMeta(body []byte) (requestMeta, bool) {
	var m requestMeta
	if err := json.Unmarshal(body, &m); err != nil {
		return requestMeta{}, false
	}
	return m, true
}

// injectIncludeUsage sets stream_options.include_usage=true on an OpenAI
// streaming request body that doesn't already have it true (cliproxyapi
// precedent: internal/runtime/executor/openai_compat_executor.go:355),
// merging into any existing stream_options rather than clobbering it. If
// include_usage is already true, or body isn't a JSON object, body is
// returned unchanged -- byte-identical, satisfying the "client's own
// include_usage: request body forwarded unchanged" scenario.
func injectIncludeUsage(body []byte) []byte {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return body
	}

	opts := map[string]json.RawMessage{}
	if raw, ok := top["stream_options"]; ok {
		_ = json.Unmarshal(raw, &opts)
		if v, ok := opts["include_usage"]; ok {
			var already bool
			if json.Unmarshal(v, &already) == nil && already {
				return body
			}
		}
	}

	opts["include_usage"] = json.RawMessage("true")
	encodedOpts, err := json.Marshal(opts)
	if err != nil {
		return body
	}
	top["stream_options"] = encodedOpts

	out, err := json.Marshal(top)
	if err != nil {
		return body
	}
	return out
}

// anthropicUsage is Anthropic's usage object shape, shared by
// message_start.message.usage (non-stream and stream) and
// message_delta.usage (stream only). Pointer fields distinguish "field
// absent" (nil) from "field present with value 0".
type anthropicUsage struct {
	InputTokens              *int64 `json:"input_tokens"`
	OutputTokens             *int64 `json:"output_tokens"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
}

// anthropicNonStreamBody is the top-level shape of a non-streaming
// Anthropic Messages response: just enough to reach "usage".
type anthropicNonStreamBody struct {
	Usage anthropicUsage `json:"usage"`
}

// parseAnthropicNonStream fills u from a complete, non-streaming Anthropic
// Messages response body: all four usage fields are recorded whenever
// present, per M1's "Anthropic, non-stream" extraction rule.
func parseAnthropicNonStream(body []byte, u *tokenUsage) {
	var b anthropicNonStreamBody
	if err := json.Unmarshal(body, &b); err != nil {
		return
	}
	applyAnthropicUsage(b.Usage, u, true)
}

// anthropicSSEEvent is one Anthropic streaming event's shape, covering
// both message_start (usage nested under "message") and message_delta
// (usage at the top level) -- see parseAnthropicSSEPayload.
type anthropicSSEEvent struct {
	Type    string `json:"type"`
	Message struct {
		Usage anthropicUsage `json:"usage"`
	} `json:"message"`
	Usage anthropicUsage `json:"usage"`
}

// parseAnthropicSSEPayload updates u from one Anthropic SSE event's JSON
// payload (the bytes after "data:"), per M1's "Anthropic, stream" rule:
// input/cache tokens come from message_start, unconditionally; output
// (and, for a compatible upstream that reports them there too, input/
// cache) comes from message_delta, where "the last non-zero value per
// field wins" -- see applyAnthropicUsage's overwriteZero argument.
func parseAnthropicSSEPayload(payload []byte, u *tokenUsage) {
	if !json.Valid(payload) {
		return
	}
	var e anthropicSSEEvent
	if err := json.Unmarshal(payload, &e); err != nil {
		return
	}
	switch e.Type {
	case "message_start":
		applyAnthropicUsage(e.Message.Usage, u, true)
	case "message_delta":
		applyAnthropicUsage(e.Usage, u, false)
	}
}

// applyAnthropicUsage copies whatever fields of src are present into u.
// overwriteZero true means every present field wins outright
// (message_start, and any non-stream response, is the first/only
// authoritative reading); false means a present-but-zero field is
// ignored, implementing "the last non-zero value per field wins" for
// message_delta's optional input/cache fields. Output always wins when
// present, in both modes, since message_delta.usage.output_tokens is
// Anthropic's own running cumulative total, not an optional extension.
func applyAnthropicUsage(src anthropicUsage, u *tokenUsage, overwriteZero bool) {
	if src.OutputTokens != nil {
		u.Output = src.OutputTokens
	}
	setUsageField(&u.Input, src.InputTokens, overwriteZero)
	setUsageField(&u.CacheRead, src.CacheReadInputTokens, overwriteZero)
	setUsageField(&u.CacheWrite, src.CacheCreationInputTokens, overwriteZero)
}

func setUsageField(dst **int64, src *int64, overwriteZero bool) {
	if src == nil {
		return
	}
	if !overwriteZero && *src == 0 {
		return
	}
	dst2 := *src
	*dst = &dst2
}

// openaiUsage is OpenAI Chat Completions' usage object shape, shared by
// the non-streaming response and the final streaming chunk (when
// stream_options.include_usage is set).
type openaiUsage struct {
	PromptTokens        *int64 `json:"prompt_tokens"`
	CompletionTokens    *int64 `json:"completion_tokens"`
	PromptTokensDetails *struct {
		CachedTokens *int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails *struct {
		ReasoningTokens *int64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

type openaiNonStreamBody struct {
	Usage *openaiUsage `json:"usage"`
}

// parseOpenAINonStream fills u from a complete, non-streaming OpenAI Chat
// Completions response body, per M1's "OpenAI, non-stream" extraction
// rule.
func parseOpenAINonStream(body []byte, u *tokenUsage) {
	var b openaiNonStreamBody
	if err := json.Unmarshal(body, &b); err != nil {
		return
	}
	applyOpenAIUsage(b.Usage, u)
}

// parseOpenAISSEPayload updates u from one OpenAI streaming chunk's JSON
// payload (the bytes after "data:"). Most chunks carry no "usage" field
// at all (null/absent); the final chunk does, once include_usage is set
// (by the client, or injected -- see injectIncludeUsage), per M1's
// "OpenAI, stream" extraction rule ("the final usage chunk").
func parseOpenAISSEPayload(payload []byte, u *tokenUsage) {
	if !json.Valid(payload) {
		return
	}
	var b openaiNonStreamBody
	if err := json.Unmarshal(payload, &b); err != nil {
		return
	}
	applyOpenAIUsage(b.Usage, u)
}

func applyOpenAIUsage(src *openaiUsage, u *tokenUsage) {
	if src == nil {
		return
	}
	if src.PromptTokens != nil {
		u.Input = src.PromptTokens
	}
	if src.CompletionTokens != nil {
		u.Output = src.CompletionTokens
	}
	if src.PromptTokensDetails != nil && src.PromptTokensDetails.CachedTokens != nil {
		u.CacheRead = src.PromptTokensDetails.CachedTokens
	}
	if src.CompletionTokensDetails != nil && src.CompletionTokensDetails.ReasoningTokens != nil {
		u.Reasoning = src.CompletionTokensDetails.ReasoningTokens
	}
}

// sseUsageScanner incrementally scans a streaming response body for usage
// data as it passes through the proxy, one SSE "data:" line at a time,
// without ever buffering the whole stream -- see this file's doc comment
// on responseBodyParseCap for the non-stream counterpart.
type sseUsageScanner struct {
	provider string
	pending  []byte
	Usage    *tokenUsage
}

func newSSEUsageScanner(provider string) *sseUsageScanner {
	return &sseUsageScanner{provider: provider, Usage: &tokenUsage{}}
}

// write feeds the next chunk of raw response bytes to the scanner. Chunk
// boundaries don't align with line boundaries in general (an SSE event's
// "data: {...}\n" can arrive split across two Write calls), so partial
// lines are held in pending across calls.
func (s *sseUsageScanner) write(p []byte) {
	s.pending = append(s.pending, p...)
	for {
		idx := bytes.IndexByte(s.pending, '\n')
		if idx < 0 {
			break
		}
		line := bytes.TrimRight(s.pending[:idx], "\r")
		s.pending = s.pending[idx+1:]
		s.processLine(line)
	}
	if len(s.pending) > sseLineCap {
		s.pending = nil
	}
}

func (s *sseUsageScanner) processLine(line []byte) {
	if !bytes.HasPrefix(line, []byte("data:")) {
		return
	}
	payload := bytes.TrimSpace(line[len("data:"):])
	if len(payload) == 0 || string(payload) == "[DONE]" {
		return
	}
	switch s.provider {
	case providerAnthropic:
		parseAnthropicSSEPayload(payload, s.Usage)
	case providerOpenAI:
		parseOpenAISSEPayload(payload, s.Usage)
	}
}

// cappedBuffer accumulates up to cap bytes and then gives up (Capped),
// discarding what it already had -- responseCapture's non-stream side
// copy, per M1's "over the cap, tokens are NULL" requirement. It never
// affects what's written to the actual client; see teeCaptureWriter.
type cappedBuffer struct {
	buf    bytes.Buffer
	cap    int
	Capped bool
}

func (c *cappedBuffer) write(p []byte) {
	if c.Capped {
		return
	}
	if c.buf.Len()+len(p) > c.cap {
		c.Capped = true
		c.buf.Reset()
		return
	}
	c.buf.Write(p)
}

// responseCapture is the side channel copyResponse feeds as it streams an
// upstream response to the client, so proxy.serve can extract usage after
// the copy finishes without ever delaying or buffering the real response.
// Exactly one of sse/nonStream is active, chosen once from the upstream
// response's Content-Type before the copy starts.
type responseCapture struct {
	provider  string
	isStream  bool
	sse       *sseUsageScanner
	nonStream *cappedBuffer
}

// newResponseCapture inspects resp's Content-Type to decide whether this
// is a streaming (SSE) or a plain JSON response -- the same signal a
// client SDK itself would use, and independent of what the request body
// asked for (an upstream error response to a stream:true request is still
// plain JSON, for instance).
func newResponseCapture(provider string, resp *http.Response) *responseCapture {
	ct := resp.Header.Get("Content-Type")
	c := &responseCapture{provider: provider, isStream: strings.Contains(ct, "text/event-stream")}
	if c.isStream {
		c.sse = newSSEUsageScanner(provider)
	} else {
		c.nonStream = &cappedBuffer{cap: responseBodyParseCap}
	}
	return c
}

func (c *responseCapture) write(p []byte) {
	if c.isStream {
		c.sse.write(p)
	} else {
		c.nonStream.write(p)
	}
}

// finalUsage computes the usage this capture observed, once the response
// copy has finished. For a non-stream response over responseBodyParseCap,
// every field is nil (unknown), per M1's parse-cap requirement.
func (c *responseCapture) finalUsage() *tokenUsage {
	if c.isStream {
		return c.sse.Usage
	}
	u := &tokenUsage{}
	if c.nonStream.Capped {
		return u
	}
	body := c.nonStream.buf.Bytes()
	switch c.provider {
	case providerAnthropic:
		parseAnthropicNonStream(body, u)
	case providerOpenAI:
		parseOpenAINonStream(body, u)
	}
	return u
}
