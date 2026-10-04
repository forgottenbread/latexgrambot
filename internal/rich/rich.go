// Package rich builds rich-message payloads for the Bot API 10.x rich
// message endpoints (sendRichMessage and the InputRichMessageContent used in
// inline answers). The pinned telegram-bot-api library predates the rich
// message API, so these are plain JSON-encodable values sent through
// api.MakeRequest.
package rich

// Message is the payload for InputRichMessage (and structurally for
// RichMessage). Only the blocks representation is used.
type Message struct {
	Blocks []Block `json:"blocks"`
}

// Block is a RichBlock/InputRichBlock.
type Block struct {
	Type       string `json:"type"`
	Text       any    `json:"text,omitempty"`
	Expression string `json:"expression,omitempty"`
	Size       int    `json:"size,omitempty"`
}

// Inline is a RichText inline node (mathematical_expression, bold, url...).
type Inline struct {
	Type       string `json:"type"`
	Text       any    `json:"text,omitempty"`
	Expression string `json:"expression,omitempty"`
	URL        string `json:"url,omitempty"`
}
