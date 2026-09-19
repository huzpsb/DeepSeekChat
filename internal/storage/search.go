package storage

import (
	"strings"
	"unicode"

	"hschat/internal/model"
)

const (
	// searchSnippetRadius is the rune radius of context shown around a hit.
	searchSnippetRadius = 40
	// searchMergeGap merges two occurrences in the same message field into
	// one hit when they are at most this many runes apart.
	searchMergeGap = 24
	// DefaultMaxHitsPerChat is the per-chat occurrence cap: a chat with
	// more hits keeps only the last DefaultMaxHitsPerChat of them.
	DefaultMaxHitsPerChat = 10
)

// fieldRef is one searchable text of a message. runes (lowered) and orig
// (original) are index-aligned, so a match position found in runes maps
// directly onto orig for snippet extraction.
type fieldRef struct {
	field string
	runes []rune // lowercased text used for matching
	orig  []rune // original text used for snippets
}

// occurrence is a single match of the query.
type occurrence struct {
	index  int // message index within the chat
	role   string
	field  string
	ref    *fieldRef
	pos    int // rune offset in ref
	length int // rune length of the match
}

// SearchChats performs a case-insensitive full-text search over every
// stored chat and returns per-chat results in chat-list order (most
// recently modified first).
//
// Scope: message content and reasoning are always searched; when
// includeTool is true, tool results (role "tool" content) and tool calls
// (function name + arguments) are searched as well. A chat whose title
// matches is returned even without message hits (TitleMatch set).
//
// Occurrences are counted per chat — one message can contribute several.
// When a chat has more than maxHits occurrences, only the LAST maxHits are
// kept (later hits have priority), Truncated is set, and TotalHits reports
// the true count. Kept occurrences that lie close together in the same
// message field are merged into one SearchHit with Count > 1.
func SearchChats(query string, includeTool bool, maxHits int) []model.ChatSearchResult {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	if maxHits <= 0 {
		maxHits = DefaultMaxHitsPerChat
	}
	needle := lowerRunes(query)

	chats, err := ListChats()
	if err != nil {
		return nil
	}

	results := make([]model.ChatSearchResult, 0)
	for i := range chats {
		if res, ok := searchChat(&chats[i], needle, includeTool, maxHits); ok {
			if res.Hits == nil {
				// title-only match: marshal as [] instead of null so
				// clients can iterate unconditionally
				res.Hits = []model.SearchHit{}
			}
			results = append(results, res)
		}
	}
	return results
}

func lowerRunes(s string) []rune {
	rs := []rune(s)
	for i, r := range rs {
		rs[i] = unicode.ToLower(r)
	}
	return rs
}

func searchChat(chat *model.Chat, needle []rune, includeTool bool, maxHits int) (model.ChatSearchResult, bool) {
	res := model.ChatSearchResult{
		Title:      chat.Title,
		TitleMatch: containsRunes(lowerRunes(chat.Title), needle),
	}

	var occs []occurrence
	for idx := range chat.Messages {
		msg := &chat.Messages[idx]
		for _, ref := range searchableFields(msg, includeTool) {
			occs = append(occs, findOccurrences(ref, idx, msg.Role, needle)...)
		}
	}

	res.TotalHits = len(occs)
	if res.TotalHits == 0 {
		return res, res.TitleMatch // title-only match is still a result
	}
	if res.TotalHits > maxHits {
		occs = occs[res.TotalHits-maxHits:]
		res.Truncated = true
	}
	res.Hits = mergeOccurrences(occs)
	return res, true
}

// searchableFields returns the texts of a message that are in scope.
// Tool results and tool calls are only searched when includeTool is set.
func searchableFields(msg *model.Message, includeTool bool) []*fieldRef {
	var refs []*fieldRef
	isTool := msg.Role == "tool"
	if !isTool || includeTool {
		if msg.Content != "" {
			refs = append(refs, newFieldRef(model.SearchFieldContent, msg.Content))
		}
		if msg.ReasoningContent != "" {
			refs = append(refs, newFieldRef(model.SearchFieldReasoning, msg.ReasoningContent))
		}
	}
	if includeTool {
		for _, tc := range msg.ToolCalls {
			refs = append(refs, newFieldRef(model.SearchFieldToolCall, tc.Function.Name+" "+tc.Function.Arguments))
		}
	}
	return refs
}

func newFieldRef(field, text string) *fieldRef {
	orig := []rune(text)
	low := make([]rune, len(orig))
	for i, r := range orig {
		low[i] = unicode.ToLower(r)
	}
	return &fieldRef{field: field, runes: low, orig: orig}
}

// findOccurrences returns all non-overlapping matches of needle in ref.
func findOccurrences(ref *fieldRef, msgIdx int, role string, needle []rune) []occurrence {
	var occs []occurrence
	low := ref.runes
	n := len(needle)
	for i := 0; i+n <= len(low); {
		if runesEqual(low[i:i+n], needle) {
			occs = append(occs, occurrence{index: msgIdx, role: role, field: ref.field, ref: ref, pos: i, length: n})
			i += n
		} else {
			i++
		}
	}
	return occs
}

func containsRunes(hay, needle []rune) bool {
	n := len(needle)
	for i := 0; i+n <= len(hay); i++ {
		if runesEqual(hay[i:i+n], needle) {
			return true
		}
	}
	return false
}

func runesEqual(a, b []rune) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// mergeOccurrences groups consecutive occurrences that share the same
// message field and lie close together into single hits with one snippet.
func mergeOccurrences(occs []occurrence) []model.SearchHit {
	var hits []model.SearchHit
	start := 0
	for i := 1; i <= len(occs); i++ {
		if i < len(occs) && mergeable(&occs[start], &occs[i-1], &occs[i]) {
			continue
		}
		hits = append(hits, buildHit(&occs[start], &occs[i-1], i-start))
		start = i
	}
	return hits
}

// mergeable reports whether cur extends the cluster that starts at first
// and currently ends at prev: same message field, a small gap after prev,
// and a total span that still fits in one snippet window.
func mergeable(first, prev, cur *occurrence) bool {
	if cur.index != first.index || cur.ref != first.ref {
		return false
	}
	if cur.pos-(prev.pos+prev.length) > searchMergeGap {
		return false
	}
	return cur.pos+cur.length-first.pos <= 2*searchSnippetRadius
}

// buildHit turns the cluster [first, last] into one SearchHit whose
// snippet covers the whole cluster plus a context radius. Newlines and
// other whitespace runs are collapsed so the snippet stays one line.
func buildHit(first, last *occurrence, count int) model.SearchHit {
	orig := first.ref.orig
	lo := first.pos - searchSnippetRadius
	if lo < 0 {
		lo = 0
	}
	hi := last.pos + last.length + searchSnippetRadius
	if hi > len(orig) {
		hi = len(orig)
	}
	var b strings.Builder
	if lo > 0 {
		b.WriteRune('…')
	}
	b.WriteString(strings.Join(strings.Fields(string(orig[lo:hi])), " "))
	if hi < len(orig) {
		b.WriteRune('…')
	}
	return model.SearchHit{
		Index:   first.index,
		Role:    first.role,
		Field:   first.field,
		Count:   count,
		Snippet: b.String(),
	}
}
