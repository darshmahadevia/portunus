package wire

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type ChatRequest struct {
	Messages        []Message `json:"messages"`
	Stream          bool      `json:"stream"`
	Model           string    `json:"model"`
	MaxOutputTokens int       `json:"max_output_tokens"`
	Temperature     float64   `json:"temperature"`
	Cache           bool      `json:"cache"`
}
type Usage struct {
	InputTokens   int64 `json:"inputTokens"`
	OutputTokens  int64 `json:"outputTokens"`
	Authoritative bool  `json:"authoritative"`
}
type ChatResponse struct {
	Text       string `json:"text"`
	RequestID  string `json:"requestId"`
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	Usage      Usage  `json:"usage"`
	CostMicros string `json:"costMicros"`
	CacheHit   bool   `json:"cacheHit"`
}
type Delta struct {
	Text string `json:"text"`
}
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type ErrorResponse struct {
	Error APIError `json:"error"`
}
