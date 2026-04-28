package line

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/line/line-bot-sdk-go/v8/linebot/messaging_api"
	"github.com/thaiaiagent/go-core/internal/thaitime"
)

// Message is an alias so callers do not import messaging_api directly.
type Message = messaging_api.MessageInterface

// Client wraps the LINE Messaging API.  It intentionally exposes only
// reply-based methods.  Push, multicast, and broadcast endpoints are omitted
// by design: LINE OA policy and our Terms of Service prohibit proactive sends.
type Client struct {
	api    *messaging_api.MessagingApiAPI
	blob   *messaging_api.MessagingApiBlobAPI
	logger *slog.Logger
}

// New constructs a Client.  httpClient may be nil to use http.DefaultClient.
func New(channelAccessToken string, httpClient *http.Client, logger *slog.Logger) (*Client, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	apiOpts := []messaging_api.MessagingApiAPIOption{
		messaging_api.WithHTTPClient(httpClient),
	}
	api, err := messaging_api.NewMessagingApiAPI(channelAccessToken, apiOpts...)
	if err != nil {
		return nil, fmt.Errorf("line client: messaging api: %w", err)
	}

	blobOpts := []messaging_api.MessagingApiBlobAPIOption{
		messaging_api.WithBlobHTTPClient(httpClient),
	}
	blob, err := messaging_api.NewMessagingApiBlobAPI(channelAccessToken, blobOpts...)
	if err != nil {
		return nil, fmt.Errorf("line client: blob api: %w", err)
	}

	return &Client{api: api, blob: blob, logger: logger}, nil
}

// ReplyText sends a single text message via the replyToken obtained from an
// inbound webhook event.  Text is capped at 4900 chars to stay within LINE's
// 5000-char limit with room for multi-byte truncation.
func (c *Client) ReplyText(ctx context.Context, replyToken, text string) error {
	return c.ReplyMessages(ctx, replyToken, textMessages(text, 5))
}

func (c *Client) ReplyImage(ctx context.Context, replyToken, text, imageURL string) error {
	messages := textMessages(text, 4)
	messages = append(messages,
		&messaging_api.ImageMessage{
			Message:            messaging_api.Message{Type: "image"},
			OriginalContentUrl: imageURL,
			PreviewImageUrl:    imageURL,
		},
	)
	return c.ReplyMessages(ctx, replyToken, messages)
}

func (c *Client) ReplyAccountingCard(ctx context.Context, replyToken, direction, note string, amount float64, category, occurredAt, projectText string) error {
	return c.ReplyMessages(ctx, replyToken, []Message{
		buildAccountingCard(direction, note, amount, category, occurredAt, projectText),
	})
}

func (c *Client) ReplySummaryCard(ctx context.Context, replyToken, title, body string) error {
	return c.ReplyMessages(ctx, replyToken, []Message{
		buildSummaryCard(title, body),
	})
}

func (c *Client) ReplyOpenAppCard(ctx context.Context, replyToken, appURL string) error {
	return c.ReplyMessages(ctx, replyToken, []Message{
		BuildOpenAppCard(appURL),
	})
}

// ReplyTextWithChoices sends a text reply with LINE quick reply message actions.
// It is still a reply-token flow, not push/multicast/broadcast.
func (c *Client) ReplyTextWithChoices(ctx context.Context, replyToken, text string, choices []string) error {
	return c.ReplyTextsWithChoices(ctx, replyToken, nil, text, choices)
}

// ReplyTextsWithChoices sends optional lead-in text messages followed by a
// final text message carrying quick reply buttons.
func (c *Client) ReplyTextsWithChoices(ctx context.Context, replyToken string, leadTexts []string, text string, choices []string) error {
	if len([]rune(text)) > 4900 {
		runes := []rune(text)
		text = string(runes[:4900])
	}
	messages := make([]Message, 0, len(leadTexts)+1)
	for _, lead := range leadTexts {
		lead = strings.TrimSpace(lead)
		if lead == "" {
			continue
		}
		if len([]rune(lead)) > 4900 {
			runes := []rune(lead)
			lead = string(runes[:4900])
		}
		messages = append(messages, &messaging_api.TextMessage{
			Message: messaging_api.Message{Type: "text"},
			Text:    lead,
		})
	}
	if len(choices) > 0 {
		messages = append(messages, buildChoiceFlex(text, choices))
	} else {
		if len([]rune(text)) > 4900 {
			runes := []rune(text)
			text = string(runes[:4900])
		}
		messages = append(messages, &messaging_api.TextMessage{
			Message: messaging_api.Message{Type: "text"},
			Text:    text,
		})
	}
	return c.ReplyMessages(ctx, replyToken, messages)
}

func buildChoiceFlex(text string, choices []string) Message {
	buttons := make([]messaging_api.FlexComponentInterface, 0, len(choices))
	for i, choice := range choices {
		label := choice
		if len([]rune(label)) > 40 {
			runes := []rune(label)
			label = string(runes[:40])
		}
		style := messaging_api.FlexButtonSTYLE_SECONDARY
		color := "#64748B"
		if i == 0 {
			style = messaging_api.FlexButtonSTYLE_PRIMARY
			color = "#16A34A"
		}
		if strings.Contains(choice, "รายจ่าย") {
			color = "#DC2626"
		}
		if strings.Contains(choice, "ไม่บันทึก") || strings.Contains(choice, "โอน") {
			style = messaging_api.FlexButtonSTYLE_SECONDARY
			color = "#64748B"
		}
		buttons = append(buttons, &messaging_api.FlexButton{
			FlexComponent: messaging_api.FlexComponent{Type: "button"},
			Style:         style,
			Color:         color,
			Height:        messaging_api.FlexButtonHEIGHT_SM,
			Action: &messaging_api.MessageAction{
				Label: label,
				Text:  choice,
			},
		})
	}
	bubble := &messaging_api.FlexBubble{
		FlexContainer: messaging_api.FlexContainer{Type: "bubble"},
		Size:          messaging_api.FlexBubbleSIZE_MEGA,
		Body: &messaging_api.FlexBox{
			FlexComponent: messaging_api.FlexComponent{Type: "box"},
			Layout:        messaging_api.FlexBoxLAYOUT_VERTICAL,
			PaddingAll:    "18px",
			Spacing:       "md",
			Contents: []messaging_api.FlexComponentInterface{
				&messaging_api.FlexText{
					FlexComponent: messaging_api.FlexComponent{Type: "text"},
					Text:          "เลขาขอเช็กก่อน",
					Weight:        messaging_api.FlexTextWEIGHT_BOLD,
					Size:          "lg",
					Color:         "#0F172A",
				},
				&messaging_api.FlexText{
					FlexComponent: messaging_api.FlexComponent{Type: "text"},
					Text:          text,
					Wrap:          true,
					Size:          "sm",
					Color:         "#475569",
				},
				flexText("เวลา: "+thaitime.ShortDateTime(thaitime.Now()), "xs", "#64748B", false, false),
			},
		},
		Footer: &messaging_api.FlexBox{
			FlexComponent: messaging_api.FlexComponent{Type: "box"},
			Layout:        messaging_api.FlexBoxLAYOUT_VERTICAL,
			Spacing:       "sm",
			Contents:      buttons,
		},
	}
	return &messaging_api.FlexMessage{
		Message:  messaging_api.Message{Type: "flex"},
		AltText:  text,
		Contents: bubble,
	}
}

func buildAccountingCard(direction, note string, amount float64, category, occurredAt, projectText string) Message {
	direction = strings.TrimSpace(direction)
	if direction == "" {
		direction = "รายการ"
	}
	note = strings.TrimSpace(note)
	if note == "" {
		note = "ไม่ระบุ"
	}
	category = strings.TrimSpace(category)
	if category == "" {
		category = "อื่น ๆ"
	}
	occurredAt = strings.TrimSpace(occurredAt)
	if occurredAt == "" {
		occurredAt = "-"
	}
	accent := "#16A34A"
	if strings.Contains(direction, "จ่าย") {
		accent = "#DC2626"
	}
	amountText := formatMoney(amount)
	contents := []messaging_api.FlexComponentInterface{
		flexText("thClaws", "xs", "#64748B", true, false),
		flexText("บันทึกรายการแล้วครับ", "lg", "#0F172A", true, false),
		flexText(fmt.Sprintf("%s %s บาท", note, amountText), "xl", "#111827", true, true),
		flexText("หมวดหมู่: "+category, "sm", "#334155", false, true),
		flexText("ประเภท: "+direction, "sm", accent, true, false),
		flexText("วันที่: "+occurredAt, "sm", "#475569", false, false),
	}
	projectText = strings.TrimSpace(projectText)
	if projectText != "" {
		contents = append(contents, flexText("โครงการ: "+projectText, "sm", "#2563EB", true, true))
	}
	contents = append(contents, flexText("บันทึกเรียบร้อยแล้วครับ", "sm", "#15803D", true, false))
	body := &messaging_api.FlexBox{
		FlexComponent: messaging_api.FlexComponent{Type: "box"},
		Layout:        messaging_api.FlexBoxLAYOUT_VERTICAL,
		PaddingAll:    "18px",
		Spacing:       "md",
		Contents:      contents,
	}
	return &messaging_api.FlexMessage{
		Message: messaging_api.Message{Type: "flex"},
		AltText: fmt.Sprintf("บันทึก%s %s %s บาทแล้ว", direction, note, amountText),
		Contents: &messaging_api.FlexBubble{
			FlexContainer: messaging_api.FlexContainer{Type: "bubble"},
			Size:          messaging_api.FlexBubbleSIZE_MEGA,
			Body:          body,
		},
	}
}

func formatMoney(value float64) string {
	negative := value < 0
	if negative {
		value = -value
	}
	parts := strings.SplitN(fmt.Sprintf("%.2f", value), ".", 2)
	whole := parts[0]
	frac := ""
	if len(parts) == 2 {
		frac = strings.TrimRight(parts[1], "0")
	}
	for i := len(whole) - 3; i > 0; i -= 3 {
		whole = whole[:i] + "," + whole[i:]
	}
	if frac != "" {
		whole += "." + frac
	}
	if negative && whole != "0" {
		whole = "-" + whole
	}
	return whole
}

func buildSummaryCard(title, bodyText string) Message {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "สรุปการเงิน"
	}
	bodyText = strings.TrimSpace(bodyText)
	if bodyText == "" {
		bodyText = "ยังไม่มีข้อมูลที่บันทึกไว้ครับ"
	}
	return &messaging_api.FlexMessage{
		Message: messaging_api.Message{Type: "flex"},
		AltText: title,
		Contents: &messaging_api.FlexBubble{
			FlexContainer: messaging_api.FlexContainer{Type: "bubble"},
			Size:          messaging_api.FlexBubbleSIZE_MEGA,
			Body: &messaging_api.FlexBox{
				FlexComponent: messaging_api.FlexComponent{Type: "box"},
				Layout:        messaging_api.FlexBoxLAYOUT_VERTICAL,
				PaddingAll:    "18px",
				Spacing:       "md",
				Contents: []messaging_api.FlexComponentInterface{
					flexText("thClaws", "xs", "#64748B", true, false),
					flexText(title, "lg", "#0F172A", true, true),
					flexText("อัปเดต: "+thaitime.ShortDateTime(thaitime.Now()), "xs", "#64748B", false, false),
					flexText(bodyText, "sm", "#334155", false, true),
				},
			},
		},
	}
}

func BuildOpenAppCard(appURL string) Message {
	title := "เปิดสมุดบัญชีครัวเรือน"
	body := "ดูรายรับ รายจ่าย กราฟ และบัญชีแยกประเภทของคุณในหน้าเดียว ข้อมูลแยกตาม LINE ID นี้เท่านั้น"
	return &messaging_api.FlexMessage{
		Message: messaging_api.Message{Type: "flex"},
		AltText: title,
		Contents: &messaging_api.FlexBubble{
			FlexContainer: messaging_api.FlexContainer{Type: "bubble"},
			Size:          messaging_api.FlexBubbleSIZE_MEGA,
			Body: &messaging_api.FlexBox{
				FlexComponent: messaging_api.FlexComponent{Type: "box"},
				Layout:        messaging_api.FlexBoxLAYOUT_VERTICAL,
				PaddingAll:    "18px",
				Spacing:       "md",
				Contents: []messaging_api.FlexComponentInterface{
					flexText("ThaiAiAgent", "xs", "#0F766E", true, false),
					flexText("เลขาบัญชีครัวเรือน", "xl", "#0F172A", true, true),
					flexText(body, "sm", "#475569", false, true),
					flexText("เวลา: "+thaitime.ShortDateTime(thaitime.Now()), "xs", "#64748B", false, false),
				},
			},
			Footer: &messaging_api.FlexBox{
				FlexComponent: messaging_api.FlexComponent{Type: "box"},
				Layout:        messaging_api.FlexBoxLAYOUT_VERTICAL,
				Spacing:       "sm",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexButton{
						FlexComponent: messaging_api.FlexComponent{Type: "button"},
						Style:         messaging_api.FlexButtonSTYLE_PRIMARY,
						Color:         "#0F766E",
						Action: &messaging_api.UriAction{
							Label: "เปิด Mini App",
							Uri:   appURL,
						},
					},
					&messaging_api.FlexButton{
						FlexComponent: messaging_api.FlexComponent{Type: "button"},
						Style:         messaging_api.FlexButtonSTYLE_SECONDARY,
						Action: &messaging_api.MessageAction{
							Label: "สรุปวันนี้",
							Text:  "สรุปวันนี้",
						},
					},
				},
			},
		},
	}
}

func flexText(text, size, color string, bold bool, wrap bool) *messaging_api.FlexText {
	weight := messaging_api.FlexTextWEIGHT_REGULAR
	if bold {
		weight = messaging_api.FlexTextWEIGHT_BOLD
	}
	return &messaging_api.FlexText{
		FlexComponent: messaging_api.FlexComponent{Type: "text"},
		Text:          text,
		Wrap:          wrap,
		Weight:        weight,
		Size:          size,
		Color:         color,
	}
}

func textMessages(text string, maxMessages int) []Message {
	if maxMessages <= 0 {
		maxMessages = 1
	}
	text = withTimestamp(text)
	parts := splitText(text, 4500, maxMessages)
	messages := make([]Message, 0, len(parts))
	for _, part := range parts {
		messages = append(messages, &messaging_api.TextMessage{
			Message: messaging_api.Message{Type: "text"},
			Text:    part,
		})
	}
	return messages
}

func withTimestamp(text string) string {
	text = strings.TrimSpace(text)
	stamp := thaitime.ShortDateTime(thaitime.Now())
	if text == "" {
		return "เวลา: " + stamp + "\nรับข้อความแล้วครับ"
	}
	if strings.HasPrefix(text, "เวลา: ") || strings.HasPrefix(text, "อัปเดต: ") {
		return text
	}
	return "เวลา: " + stamp + "\n" + text
}

func splitText(text string, limit, maxParts int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return []string{"รับข้อความแล้วครับ"}
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return []string{text}
	}
	parts := make([]string, 0, maxParts)
	for len(runes) > 0 && len(parts) < maxParts {
		take := limit
		if len(runes) < take {
			take = len(runes)
		}
		if take < len(runes) {
			for i := take; i > take-300 && i > 0; i-- {
				if runes[i-1] == '\n' || runes[i-1] == ' ' {
					take = i
					break
				}
			}
		}
		part := strings.TrimSpace(string(runes[:take]))
		if part != "" {
			parts = append(parts, part)
		}
		runes = runes[take:]
	}
	if len(runes) > 0 && len(parts) > 0 {
		last := []rune(parts[len(parts)-1])
		suffix := "\n\nข้อความยาวมากครับ ตอบได้เท่านี้ก่อน ถ้าต้องการต่อ พิมพ์ว่า \"ต่อ\" ได้เลย"
		keep := limit - len([]rune(suffix))
		if keep > 0 && len(last) > keep {
			last = last[:keep]
		}
		parts[len(parts)-1] = strings.TrimSpace(string(last)) + suffix
	}
	return parts
}

// ReplyMessages sends up to 5 messages using a replyToken.  replyToken is
// consumed by LINE after the first successful call; callers must not retry.
func (c *Client) ReplyMessages(ctx context.Context, replyToken string, messages []Message) error {
	req := &messaging_api.ReplyMessageRequest{
		ReplyToken: replyToken,
		Messages:   messages,
	}
	// The SDK's ReplyMessage does not accept a context.  We honour the
	// deadline by checking it before the call and relying on the transport
	// layer (http.Client with context) for cancellation.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("line reply: context: %w", err)
	}
	if _, err := c.api.ReplyMessage(req); err != nil {
		return fmt.Errorf("line reply: %w", err)
	}
	return nil
}

// GetMessageContent downloads binary content (image, video, audio) from the
// LINE content delivery endpoint.  The caller is responsible for closing the
// returned reader and must not persist the bytes as base64 — store to MinIO
// as raw bytes, pass base64 only at LLM request time.
func (c *Client) GetMessageContent(ctx context.Context, messageID string) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", fmt.Errorf("line content: context: %w", err)
	}
	resp, err := c.blob.GetMessageContent(messageID)
	if err != nil {
		return nil, "", fmt.Errorf("line content: fetch: %w", err)
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("line content: read body: %w", err)
	}
	return data, contentType, nil
}
