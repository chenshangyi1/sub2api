package service

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	openAIStreamClientIdentityKey = "openai_stream_client_identity"
	openAIStreamPreambleBytesKey  = "openai_stream_preamble_bytes"
)

// openAIStreamClientIdentity remembers the Responses identity already shown to
// the client so a silent same-connection failover can keep that id while a
// later account continues the stream.
type openAIStreamClientIdentity struct {
	responseID     string
	createdSent    bool
	inProgressSent bool
	lastSequence   int64
	hasSequence    bool
}

func openAIStreamClientIdentityFrom(c *gin.Context) *openAIStreamClientIdentity {
	if c == nil {
		return nil
	}
	if value, ok := c.Get(openAIStreamClientIdentityKey); ok {
		if ident, valid := value.(*openAIStreamClientIdentity); valid && ident != nil {
			return ident
		}
	}
	ident := &openAIStreamClientIdentity{lastSequence: -1}
	c.Set(openAIStreamClientIdentityKey, ident)
	return ident
}

func openAIStreamPublishedResponseID(c *gin.Context) string {
	ident := openAIStreamClientIdentityFrom(c)
	if ident == nil {
		return ""
	}
	return ident.responseID
}

func recordOpenAIStreamPreambleBytes(c *gin.Context, written int) {
	if c == nil || written <= 0 {
		return
	}
	current := 0
	if value, ok := c.Get(openAIStreamPreambleBytesKey); ok {
		current, _ = value.(int)
	}
	c.Set(openAIStreamPreambleBytesKey, current+written)
}

// RecordOpenAIStreamPreambleBytes lets handlers account flushed OpenAI
// preamble (response.created / in_progress) the same way streaming paths do:
// those bytes are rewritten onto the client identity during silent failover
// and must not freeze the attempt like a visible token would.
func RecordOpenAIStreamPreambleBytes(c *gin.Context, written int) {
	recordOpenAIStreamPreambleBytes(c, written)
}

func openAIStreamPreambleBytes(c *gin.Context) int {
	if c == nil {
		return 0
	}
	if value, ok := c.Get(openAIStreamPreambleBytesKey); ok {
		n, _ := value.(int)
		if n > 0 {
			return n
		}
	}
	return 0
}

func shouldSuppressOpenAIStreamEvent(c *gin.Context, eventType string) bool {
	ident := openAIStreamClientIdentityFrom(c)
	if ident == nil || ident.responseID == "" {
		return false
	}
	switch strings.TrimSpace(eventType) {
	case "response.created":
		return ident.createdSent
	case "response.in_progress":
		return ident.inProgressSent
	default:
		return false
	}
}

func noteOpenAIStreamPreambleFlushed(c *gin.Context, eventType, responseID string, sequence int64, hasSequence bool) {
	ident := openAIStreamClientIdentityFrom(c)
	if ident == nil {
		return
	}
	if responseID = strings.TrimSpace(responseID); responseID != "" && ident.responseID == "" {
		ident.responseID = responseID
	}
	switch strings.TrimSpace(eventType) {
	case "response.created":
		ident.createdSent = true
	case "response.in_progress":
		ident.inProgressSent = true
	}
	if hasSequence {
		ident.hasSequence = true
		if sequence > ident.lastSequence {
			ident.lastSequence = sequence
		}
	}
}

func openAIStreamFrameSequence(frame openAISSEDataFrame) (int64, bool) {
	if !frame.validJSON {
		return 0, false
	}
	seq := frame.root.Get("sequence_number")
	if !seq.Exists() {
		return 0, false
	}
	return seq.Int(), true
}

func applyOpenAIStreamClientIdentity(c *gin.Context, line string, frame openAISSEDataFrame) (string, openAISSEDataFrame, bool) {
	ident := openAIStreamClientIdentityFrom(c)
	if ident == nil || ident.responseID == "" {
		return line, frame, false
	}
	if shouldSuppressOpenAIStreamEvent(c, frame.eventType) {
		return line, frame, true
	}
	upstreamID := extractOpenAIResponseIDFromSSEFrame(frame)
	if upstreamID != "" && upstreamID != ident.responseID {
		line, frame = rewriteOpenAIStreamResponseID(line, frame, upstreamID, ident.responseID)
	}
	line, frame = rewriteOpenAIStreamSequence(line, frame, ident)
	return line, frame, false
}

func rewriteOpenAIStreamResponseID(line string, frame openAISSEDataFrame, fromID, toID string) (string, openAISSEDataFrame) {
	data, ok := extractOpenAISSEDataLine(line)
	if !ok || data == "" || data == "[DONE]" {
		return line, frame
	}
	body := data
	if gjson.Get(body, "id").String() == fromID {
		if rewritten, err := sjson.Set(body, "id", toID); err == nil {
			body = rewritten
		}
	}
	if gjson.Get(body, "response.id").String() == fromID {
		if rewritten, err := sjson.Set(body, "response.id", toID); err == nil {
			body = rewritten
		}
	}
	if body == data {
		return line, frame
	}
	return "data: " + body, parseTrustedOpenAISSEDataFrame([]byte(body), frame.eventType)
}

func rewriteOpenAIStreamSequence(line string, frame openAISSEDataFrame, ident *openAIStreamClientIdentity) (string, openAISSEDataFrame) {
	if ident == nil || !ident.hasSequence || !frame.validJSON {
		return line, frame
	}
	seq := frame.root.Get("sequence_number")
	if !seq.Exists() {
		return line, frame
	}
	next := ident.lastSequence + 1
	current := seq.Int()
	if current == next {
		ident.lastSequence = next
		return line, frame
	}
	data, ok := extractOpenAISSEDataLine(line)
	if !ok || data == "" || data == "[DONE]" {
		return line, frame
	}
	rewritten, err := sjson.Set(data, "sequence_number", next)
	if err != nil {
		return line, frame
	}
	ident.lastSequence = next
	return "data: " + rewritten, parseTrustedOpenAISSEDataFrame([]byte(rewritten), frame.eventType)
}

func markOpenAIStreamFailoverSafeAfterPreamble(c *gin.Context, failoverErr *UpstreamFailoverError) {
	if failoverErr == nil || openAIStreamClientOutputStarted(c, false) {
		return
	}
	failoverErr.SafeToFailoverAfterWrite = true
}
