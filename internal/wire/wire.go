package wire

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type ChatResponse struct {
	Text string `json:"text"`
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
