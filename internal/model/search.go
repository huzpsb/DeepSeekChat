package model

// Fields covered by chat full-text search. Field tells the client where in
// the message a hit is, so the UI can label it.
const (
	SearchFieldContent   = "content"   // message content (tool results only when tools are included)
	SearchFieldReasoning = "reasoning" // assistant reasoning content
	SearchFieldToolCall  = "tool_call" // tool call function name + arguments
)

// SearchHit is one returned match: a single occurrence, or several
// occurrences that lie close together in the same message field merged
// into one snippet (Count > 1).
type SearchHit struct {
	Index   int    `json:"index"` // message index within the chat
	Role    string `json:"role"`
	Field   string `json:"field"`
	Count   int    `json:"count"`   // occurrences merged into this hit
	Snippet string `json:"snippet"` // match context, whitespace collapsed
}

// ChatSearchResult is the search outcome for a single chat.
type ChatSearchResult struct {
	Title      string      `json:"title"`
	TitleMatch bool        `json:"title_match,omitempty"` // query also matches the chat title
	TotalHits  int         `json:"total_hits"`            // total occurrences, before truncation
	Truncated  bool        `json:"truncated,omitempty"`   // more hits than the cap: the LAST ones are kept
	Hits       []SearchHit `json:"hits"`
}
