// Package dispatcher implements the queue.LineEventDispatcher contract: it
// reads a LINE event payload, resolves the LINE userId to an existing
// User+Agent row (Node remains the writer for those during the strangler
// migration), runs the deterministic Thai parser, persists ledger writes,
// renders today/month summaries, and replies via the LINE messaging API.
package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/thaiaiagent/go-core/internal/ledger"
	"github.com/thaiaiagent/go-core/internal/liff"
	"github.com/thaiaiagent/go-core/internal/media"
	"github.com/thaiaiagent/go-core/internal/memory"
	"github.com/thaiaiagent/go-core/internal/parser"
	"github.com/thaiaiagent/go-core/internal/queue"
	"github.com/thaiaiagent/go-core/internal/thaitime"
	"github.com/thaiaiagent/go-core/internal/users"
	"github.com/thaiaiagent/go-core/internal/websearch"
)

var thaiAmountOnlyPattern = regexp.MustCompile(`^(ศูนย์|หนึ่ง|นึง|เอ็ด|สอง|สาม|สี่|ห้า|หก|เจ็ด|แปด|เก้า|สิบ|ยี่|ร้อย|พัน|หมื่น|แสน|ล้าน)+$`)

const (
	thClawsRuntime   = "thclaws-session-broker"
	thClawsModel     = "sml/auto"
	timeoutReplyText = "หมดเวลาประมวลผลครับ กรุณาส่งใหม่อีกครั้งนะครับ"
)

type thClawsSessionContextKey struct{}

type thClawsSessionInfo struct {
	ID      string
	Runtime string
	Model   string
}

type Replier interface {
	ReplyText(ctx context.Context, replyToken, text string) error
	ReplyImage(ctx context.Context, replyToken, text, imageURL string) error
	ReplyAccountingCard(ctx context.Context, replyToken, direction, note string, amount float64, category, occurredAt, projectText string) error
	ReplySummaryCard(ctx context.Context, replyToken, title, body string) error
	ReplyOpenAppCard(ctx context.Context, replyToken, appURL string) error
	ReplyTextWithChoices(ctx context.Context, replyToken, text string, choices []string) error
	ReplyTextsWithChoices(ctx context.Context, replyToken string, leadTexts []string, text string, choices []string) error
}

type GatewayChatter interface {
	Chat(ctx context.Context, namespace, sessionID, model, fastModel, generalModel string, prompt string) (string, error)
}

type LineContentGetter interface {
	GetMessageContent(ctx context.Context, messageID string) ([]byte, string, error)
}

type ObjectStore interface {
	Put(ctx context.Context, objectName string, data []byte, contentType string) (bucket string, err error)
	Get(ctx context.Context, bucket, objectName string) ([]byte, string, error)
}

type VisionAnalyzer interface {
	AnalyzeImage(ctx context.Context, namespace, sessionID string, data []byte, contentType string) (parser.VisionResult, error)
}

type JobEnqueuer interface {
	EnqueueMemoryCompact(ctx context.Context, job queue.MemoryCompactJob) error
	EnqueueBrainExtract(ctx context.Context, job queue.BrainExtractJob) error
}

type WebSearcher interface {
	Search(ctx context.Context, text string, limit int) ([]websearch.Result, error)
}

type Service struct {
	replier           Replier
	chatter           GatewayChatter
	lineContent       LineContentGetter
	store             ObjectStore
	vision            VisionAnalyzer
	jobs              JobEnqueuer
	webSearch         WebSearcher
	liffIssuer        *liff.Handler
	users             *users.Service
	memory            *memory.MemoryService
	writer            *ledger.Writer
	summary           *ledger.SummaryReader
	publicBaseURL     string
	imageAccessSecret string
	liffID            string
	replyTimeout      time.Duration
	logger            *slog.Logger
}

func New(
	replier Replier,
	chatter GatewayChatter,
	lineContent LineContentGetter,
	store ObjectStore,
	vision VisionAnalyzer,
	jobs JobEnqueuer,
	webSearch WebSearcher,
	liffIssuer *liff.Handler,
	usersSvc *users.Service,
	memorySvc *memory.MemoryService,
	writer *ledger.Writer,
	summary *ledger.SummaryReader,
	publicBaseURL string,
	imageAccessSecret string,
	liffID string,
	replyTimeout time.Duration,
	logger *slog.Logger,
) *Service {
	if replyTimeout <= 0 {
		replyTimeout = 35 * time.Second
	}
	return &Service{
		replier:           replier,
		chatter:           chatter,
		lineContent:       lineContent,
		store:             store,
		vision:            vision,
		jobs:              jobs,
		webSearch:         webSearch,
		liffIssuer:        liffIssuer,
		users:             usersSvc,
		memory:            memorySvc,
		writer:            writer,
		summary:           summary,
		publicBaseURL:     strings.TrimRight(strings.TrimSpace(publicBaseURL), "/"),
		imageAccessSecret: strings.TrimSpace(imageAccessSecret),
		liffID:            strings.TrimSpace(liffID),
		replyTimeout:      replyTimeout,
		logger:            logger,
	}
}

func (s *Service) ensureThClawsSession(ctx context.Context, id users.Identity, lastIntent string) (*memory.AgentSession, error) {
	return s.memory.EnsureAgentSession(ctx, memory.EnsureAgentSessionInput{
		OwnerID:    id.UserID,
		AgentID:    id.AgentID,
		Namespace:  id.Namespace,
		Runtime:    thClawsRuntime,
		Model:      thClawsModel,
		LastIntent: lastIntent,
	})
}

func withThClawsSession(ctx context.Context, session *memory.AgentSession) context.Context {
	if session == nil {
		return ctx
	}
	return context.WithValue(ctx, thClawsSessionContextKey{}, thClawsSessionInfo{
		ID:      session.ID.Hex(),
		Runtime: session.Runtime,
		Model:   session.Model,
	})
}

func thClawsSessionFromContext(ctx context.Context) thClawsSessionInfo {
	if ctx == nil {
		return thClawsSessionInfo{}
	}
	info, _ := ctx.Value(thClawsSessionContextKey{}).(thClawsSessionInfo)
	return info
}

func thClawsSessionID(ctx context.Context) string {
	return thClawsSessionFromContext(ctx).ID
}

func (s *Service) withSessionMetadata(ctx context.Context, meta map[string]any) map[string]any {
	if meta == nil {
		meta = map[string]any{}
	}
	info := thClawsSessionFromContext(ctx)
	if info.ID == "" {
		return meta
	}
	meta["thclawsSessionId"] = info.ID
	meta["thclawsRuntime"] = info.Runtime
	meta["thclawsModel"] = info.Model
	return meta
}

func (s *Service) Dispatch(ctx context.Context, eventJSON json.RawMessage) error {
	var env struct {
		LineUserID  string `json:"lineUserId"`
		ReplyToken  string `json:"replyToken"`
		EventType   string `json:"eventType"`
		MessageType string `json:"messageType"`
		Text        string `json:"text"`
		MessageID   string `json:"messageId"`
	}
	if err := json.Unmarshal(eventJSON, &env); err != nil {
		return fmt.Errorf("dispatcher: unmarshal event: %w", err)
	}
	s.logger.Info("dispatcher: event received",
		"eventType", env.EventType,
		"messageType", env.MessageType,
		"messageId", env.MessageID,
		"textLength", len([]rune(env.Text)),
		"hasReplyToken", env.ReplyToken != "",
		"lineUserIdHash", redactID(env.LineUserID),
	)
	if env.ReplyToken == "" {
		// follow/unfollow/postback/delivery events ship without a reply token;
		// silently ignore so we never get stuck retrying them.
		s.logger.Info("dispatcher: event without replyToken (likely non-message)",
			"eventType", env.EventType, "messageType", env.MessageType, "lineUserIdHash", redactID(env.LineUserID))
		return nil
	}
	if s.replyBudgetExceeded(ctx) {
		s.logger.Warn("dispatcher: reply budget exceeded before processing",
			"eventType", env.EventType, "messageType", env.MessageType, "lineUserIdHash", redactID(env.LineUserID))
		replyCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.replier.ReplyText(replyCtx, env.ReplyToken, timeoutReplyText)
	}

	identity, err := s.users.ResolveOrCreateLineUser(ctx, env.LineUserID)
	if err != nil {
		if errors.Is(err, users.ErrUnknown) {
			return s.replier.ReplyText(ctx, env.ReplyToken,
				"ยินดีต้อนรับ! ระบบกำลังสร้างบัญชีให้คุณ ลองส่งข้อความนี้อีกครั้งสักครู่นะครับ")
		}
		return fmt.Errorf("dispatcher: resolve user: %w", err)
	}
	s.logger.Info("dispatcher: identity resolved",
		"userId", identity.UserID,
		"agentId", identity.AgentID,
		"namespace", identity.Namespace,
		"lineUserIdHash", redactID(env.LineUserID),
	)

	switch env.MessageType {
	case "image":
		return s.handleImage(ctx, identity, env.ReplyToken, env.MessageID)
	case "text":
		return s.handleText(ctx, identity, env.LineUserID, env.ReplyToken, env.MessageID, env.Text)
	default:
		s.logger.Info("dispatcher: ignoring unsupported event", "type", env.MessageType)
		return nil
	}
}

func (s *Service) handleText(ctx context.Context, id users.Identity, lineUserID, replyToken, messageID, text string) error {
	text = strings.TrimSpace(text)
	s.logger.Info("dispatcher: text handling started",
		"userId", id.UserID,
		"namespace", id.Namespace,
		"messageId", messageID,
		"textLength", len([]rune(text)),
	)
	if _, err := s.memory.EnsureAgentProfile(ctx, id.AgentID, id.Namespace); err != nil {
		s.logger.Warn("dispatcher: ensure agent profile failed", "err", err, "namespace", id.Namespace)
	} else {
		s.logger.Info("dispatcher: agent profile ensured", "agentId", id.AgentID, "namespace", id.Namespace)
	}
	finalIntent := "unknown"
	session, sessionErr := s.ensureThClawsSession(ctx, id, "line_text")
	if sessionErr != nil {
		s.logger.Error("dispatcher: thclaws session unavailable", "err", sessionErr, "namespace", id.Namespace)
	} else if session != nil {
		ctx = withThClawsSession(ctx, session)
		s.logger.Info("dispatcher: thclaws session resumed",
			"sessionId", session.ID.Hex(),
			"runtime", session.Runtime,
			"model", session.Model,
			"turnCount", session.TurnCount,
			"namespace", id.Namespace,
		)
		defer func() {
			if err := s.memory.TouchAgentSession(ctx, id.Namespace, thClawsRuntime, finalIntent); err != nil {
				s.logger.Warn("dispatcher: thclaws session touch failed", "err", err, "namespace", id.Namespace)
			}
		}()
	}
	if msgID, err := s.memory.SaveConversationMessage(ctx, memory.SaveMessageInput{
		OwnerID: id.UserID, AgentID: id.AgentID, Namespace: id.Namespace,
		Role: "user", Content: text, Metadata: s.withSessionMetadata(ctx, map[string]any{"source": "line"}),
	}); err != nil {
		s.logger.Warn("dispatcher: user conversation save failed", "err", err, "namespace", id.Namespace)
	} else {
		s.logger.Info("dispatcher: user conversation saved", "mongoId", msgID, "namespace", id.Namespace)
	}
	if err := s.memory.SaveEventLog(ctx, memory.EventLogInput{
		OwnerID: id.UserID, AgentID: id.AgentID, Namespace: id.Namespace,
		EventType: "line.text.received", Content: text,
		Metadata: s.withSessionMetadata(ctx, map[string]any{"lineUserIdHash": redactID(lineUserID), "messageLength": len([]rune(text))}),
	}); err != nil {
		s.logger.Warn("dispatcher: text event log save failed", "err", err, "namespace", id.Namespace)
	} else {
		s.logger.Info("dispatcher: text event log saved", "eventType", "line.text.received", "namespace", id.Namespace)
	}
	s.enqueueMemoryJobs(ctx, id, text)

	if wantsLatestImageAnalysis(text) {
		finalIntent = "latest_image_analysis"
		return s.analyzeLatestImage(ctx, id, replyToken)
	}
	if wantsImageRecall(text) {
		finalIntent = "latest_image_recall"
		return s.replyLatestImage(ctx, id, replyToken)
	}
	if wantsMiniApp(text) {
		finalIntent = "open_liff"
		return s.replyOpenMiniApp(ctx, id, replyToken)
	}
	if isCapabilityQuestion(text) {
		finalIntent = "help"
		return s.replyChoices(ctx, id, replyToken, helpText(), []string{"เปิดสมุดบัญชี", "สรุปวันนี้", "สรุปเดือนนี้", "อ่านรูปล่าสุด"})
	}
	if isFrustration(text) {
		finalIntent = "frustration"
		return s.replyChoices(ctx, id, replyToken,
			"รับทราบครับ รอบก่อนตอบไม่เข้าท่า เดี๋ยวผมคุมให้แน่นขึ้น: ไม่เดา ไม่มั่ว ถ้าเป็นเรื่องบัญชีจะถามก่อนบันทึกครับ",
			[]string{"ช่วยเหลือ", "สรุปวันนี้", "ไม่บันทึก"})
	}
	if asksAssistantToGuess(text) {
		finalIntent = "ask_clarify_guess"
		return s.replyChoices(ctx, id, replyToken,
			"ผมช่วยคิดได้ครับ แต่เรื่องเงินกับข้อมูลส่วนตัวผมจะไม่เดาให้มั่ว ๆ เลือกทางที่ต้องการได้เลย",
			[]string{"สรุปวันนี้", "บันทึกรายรับ", "บันทึกรายจ่าย", "ถามทั่วไป"})
	}

	if choice := accountingChoice(text); choice != "" {
		if handled, err := s.handlePendingAccountingChoice(ctx, id, replyToken, messageID, choice); err != nil {
			finalIntent = "accounting_choice_error"
			return err
		} else if handled {
			finalIntent = "accounting_choice"
			return nil
		}
		finalIntent = "accounting_choice_without_pending"
		return s.reply(ctx, id, replyToken, "หมายถึงรายการไหนครับ? ส่งข้อความรายการมาก่อน เช่น \"กาแฟ 60\" แล้วผมจะจัดให้ ไม่มั่วแน่นอน")
	}

	if handled, err := s.handlePendingAmount(ctx, id, replyToken, messageID, text); err != nil {
		finalIntent = "pending_amount_error"
		return err
	} else if handled {
		finalIntent = "pending_amount"
		return nil
	}

	if isAmbiguousFollowUp(text) {
		s.logger.Info("dispatcher: ambiguous follow-up without pending; asking clarification", "namespace", id.Namespace, "messageId", messageID)
		finalIntent = "ask_clarify_followup"
		return s.replyChoices(ctx, id, replyToken,
			"ผมเห็นว่าเป็นคำตอบต่อเนื่อง แต่ตอนนี้ไม่มีรายการค้างให้ยืนยันครับ เลขาขอไม่เดานะ ส่งรายการใหม่อีกทีได้เลย",
			[]string{"ช่วยเหลือ", "ไม่บันทึก"})
	}

	// ── Household / Business routing ──
	if isHouseholdCommand(text) || isHouseholdSummary(text) {
		return s.handleHouseholdText(ctx, id, replyToken, messageID, text)
	}
	if isBusinessSale(text) || isBusinessPurchase(text) || isBusinessProfit(text) {
		return s.handleBusinessText(ctx, id, replyToken, messageID, text)
	}

	parsed := parser.Parse(text)
	s.logger.Info("dispatcher: text parsed",
		"intent", parsed.Intent,
		"type", parsed.Type,
		"amount", parsed.Amount,
		"category", parsed.Category,
		"confidence", parsed.Confidence,
		"reason", parsed.Reason,
		"namespace", id.Namespace,
		"messageId", messageID,
	)
	switch parsed.Intent {
	case parser.IntentIgnore:
		s.logger.Info("dispatcher: user chose ignore", "userId", id.UserID, "namespace", id.Namespace)
		if err := s.memory.ClearPendingAccounting(ctx, id.Namespace); err != nil {
			s.logger.Warn("dispatcher: clear pending after ignore failed", "err", err, "namespace", id.Namespace)
		}
		finalIntent = "ignore"
		return s.reply(ctx, id, replyToken, "โอเคครับ ไม่บันทึกรายการนี้")
	case parser.IntentRecordExpense, parser.IntentRecordIncome:
		if parsed.Confidence < 0.75 {
			s.logger.Info("dispatcher: low confidence; asking with quick reply",
				"namespace", id.Namespace, "confidence", parsed.Confidence, "messageId", messageID)
			if err := s.memory.SavePendingAccounting(ctx, memory.PendingAccountingInput{
				OwnerID: id.UserID, AgentID: id.AgentID, Namespace: id.Namespace,
				Original: text, Amount: parsed.Amount, Note: parsed.Note, Category: parsed.Category,
				CounterpartyName: parsed.CounterpartyName, CounterpartyRole: parsed.CounterpartyRole,
				Confidence: parsed.Confidence, CausationID: messageID,
			}); err != nil {
				s.logger.Warn("dispatcher: save pending accounting failed", "err", err, "namespace", id.Namespace)
			}
			finalIntent = "ask_confirm_accounting"
			return s.replyChoicesWithLead(ctx, id, replyToken,
				[]string{fmt.Sprintf("ผมอ่านได้ว่า: %s %s บาท", parsed.Note, ledger.FormatMoney(parsed.Amount))},
				pendingQuestion(parsed.Note, parsed.Amount),
				[]string{"รายรับ", "รายจ่าย", "โอนเงิน", "ไม่บันทึก"})
		}
		allocations, projects := s.resolveProjectAllocations(ctx, id, text, parsed.Amount)
		txID, err := s.writer.CreatePersonalTransaction(ctx, ledger.WriteTxInput{
			UserID:           id.UserID,
			AgentID:          id.AgentID,
			Namespace:        id.Namespace,
			Type:             string(parsed.Type),
			Amount:           parsed.Amount,
			Category:         parsed.Category,
			Note:             parsed.Note,
			CounterpartyName: parsed.CounterpartyName,
			CounterpartyRole: parsed.CounterpartyRole,
			Confidence:       parsed.Confidence,
			CausationID:      messageID,
			Allocations:      allocations,
		})
		if err != nil {
			s.logger.Error("dispatcher: ledger write failed", "err", err)
			finalIntent = "record_accounting_failed"
			return s.replier.ReplyText(ctx, replyToken, "บันทึกไม่สำเร็จ ลองอีกครั้งนะครับ")
		}
		s.logger.Info("ledger transaction stored", "txId", txID, "namespace", id.Namespace, "type", parsed.Type, "amount", parsed.Amount, "projectCount", len(projects))
		direction := "รายจ่าย"
		finalIntent = "record_expense"
		if parsed.Intent == parser.IntentRecordIncome {
			direction = "รายรับ"
			finalIntent = "record_income"
		}
		return s.replyAccountingCard(ctx, id, replyToken, direction, parsed.Note, parsed.Amount, parsed.Category, projects)
	case parser.IntentAskTodaySummary:
		s.logger.Info("dispatcher: today summary requested", "userId", id.UserID, "namespace", id.Namespace)
		summary, err := s.summary.Today(ctx, id.UserID, id.Namespace)
		if err != nil {
			s.logger.Error("dispatcher: today summary failed", "err", err)
			finalIntent = "today_summary_failed"
			return s.reply(ctx, id, replyToken, "ขอเวลาดึงข้อมูลครับ ลองใหม่อีกที")
		}
		finalIntent = "today_summary"
		return s.replySummaryCard(ctx, id, replyToken, "สรุปวันนี้", summary)
	case parser.IntentAskMonthSummary:
		s.logger.Info("dispatcher: month summary requested", "userId", id.UserID, "namespace", id.Namespace)
		summary, err := s.summary.ThisMonth(ctx, id.UserID, id.Namespace)
		if err != nil {
			s.logger.Error("dispatcher: month summary failed", "err", err)
			finalIntent = "month_summary_failed"
			return s.reply(ctx, id, replyToken, "ขอเวลาดึงข้อมูลครับ ลองใหม่อีกที")
		}
		finalIntent = "month_summary"
		return s.replySummaryCard(ctx, id, replyToken, "สรุปเดือนนี้", summary)
	case parser.IntentAskBalance:
		s.logger.Info("dispatcher: balance requested", "userId", id.UserID, "namespace", id.Namespace)
		summary, err := s.summary.Balance(ctx, id.UserID, id.Namespace)
		if err != nil {
			s.logger.Error("dispatcher: balance summary failed", "err", err)
			finalIntent = "balance_failed"
			return s.reply(ctx, id, replyToken, "ขอเวลาดึงยอดคงเหลือครับ ลองใหม่อีกที")
		}
		finalIntent = "balance"
		return s.replySummaryCard(ctx, id, replyToken, "ภาพรวมการเงิน", summary)
	case parser.IntentAskDate:
		s.logger.Info("dispatcher: date requested", "userId", id.UserID, "namespace", id.Namespace)
		finalIntent = "ask_date"
		return s.reply(ctx, id, replyToken, thaiToday())
	case parser.IntentHelp:
		s.logger.Info("dispatcher: help requested", "userId", id.UserID, "namespace", id.Namespace)
		finalIntent = "help"
		return s.replyChoices(ctx, id, replyToken, helpText(), []string{"เปิดสมุดบัญชี", "สรุปวันนี้", "สรุปเดือนนี้", "อ่านรูปล่าสุด"})
	}

	if parsed.Reason == "missing_amount" && looksAccountingRelated(text) && !isKnowledgeOrAdviceRequest(text) {
		s.logger.Info("dispatcher: missing amount; asking with quick reply", "namespace", id.Namespace, "messageId", messageID)
		if err := s.memory.SavePendingAccounting(ctx, memory.PendingAccountingInput{
			OwnerID: id.UserID, AgentID: id.AgentID, Namespace: id.Namespace,
			Original: text, Note: text, Confidence: 0.2, CausationID: messageID,
		}); err != nil {
			s.logger.Warn("dispatcher: save pending missing amount failed", "err", err, "namespace", id.Namespace)
		}
		finalIntent = "ask_amount"
		return s.replyChoicesWithLead(ctx, id, replyToken,
			[]string{"ผมจับได้ว่าเกี่ยวกับเงิน แต่ยังไม่เห็นจำนวนเงินครับ"},
			"รายการนี้เกี่ยวกับบัญชีไหมครับ? ถ้าจะบันทึก รบกวนส่งจำนวนเงินเพิ่มด้วย เช่น 60 บาท",
			[]string{"รายรับ", "รายจ่าย", "โอนเงิน", "ไม่บันทึก"})
	}

	if s.chatter == nil {
		finalIntent = "chat_unavailable"
		return s.reply(ctx, id, replyToken, "รับข้อความแล้วครับ")
	}
	sessionID := thClawsSessionID(ctx)
	if sessionID == "" {
		finalIntent = "thclaws_session_unavailable"
		return s.reply(ctx, id, replyToken, "ระบบสมองกลางกำลังต่อประวัติไม่สำเร็จครับ ลองอีกครั้งนะครับ")
	}
	opCtx, cancel := s.operationContext(ctx)
	defer cancel()
	webResults, webErr := s.searchWebIfNeeded(opCtx, id, text)
	prompt := s.buildPrompt(opCtx, id, text, webResults, webErr)
	s.logger.Info("dispatcher: chat requested",
		"namespace", id.Namespace,
		"sessionId", sessionID,
		"webResults", len(webResults),
		"webSearchFailed", webErr != nil,
		"promptLength", len([]rune(prompt)),
	)
	answer, err := s.chatter.Chat(opCtx, id.Namespace, sessionID, "sml/fast", "sml/thai", "sml/auto", prompt)
	if err != nil {
		s.logger.Warn("dispatcher: chat failed", "err", err)
		finalIntent = "chat_failed"
		if isTimeoutErr(err) {
			return s.replyTimeoutNotice(id, replyToken)
		}
		return s.reply(ctx, id, replyToken, "ระบบขัดข้องชั่วคราวครับ ลองอีกครั้งนะ")
	}
	_ = s.memory.SaveConversationSnapshot(ctx, id.Namespace, lineUserID, "chat")
	answer = sanitizeChatAnswer(answer)
	finalIntent = "chat"
	if len(webResults) > 0 {
		finalIntent = "chat_web_search"
	}
	s.logger.Info("dispatcher: chat completed", "namespace", id.Namespace, "sessionId", sessionID, "answerLength", len([]rune(answer)))
	return s.reply(ctx, id, replyToken, answer)
}

func (s *Service) handleImage(ctx context.Context, id users.Identity, replyToken, messageID string) error {
	s.logger.Info("dispatcher: image handling started", "userId", id.UserID, "namespace", id.Namespace, "messageId", messageID)
	_, _ = s.memory.EnsureAgentProfile(ctx, id.AgentID, id.Namespace)
	finalIntent := "image_received"
	session, sessionErr := s.ensureThClawsSession(ctx, id, "line_image")
	if sessionErr != nil {
		s.logger.Error("dispatcher: thclaws image session unavailable", "err", sessionErr, "namespace", id.Namespace)
	} else if session != nil {
		ctx = withThClawsSession(ctx, session)
		s.logger.Info("dispatcher: thclaws image session resumed",
			"sessionId", session.ID.Hex(),
			"runtime", session.Runtime,
			"model", session.Model,
			"turnCount", session.TurnCount,
			"namespace", id.Namespace,
		)
		defer func() {
			if err := s.memory.TouchAgentSession(ctx, id.Namespace, thClawsRuntime, finalIntent); err != nil {
				s.logger.Warn("dispatcher: thclaws image session touch failed", "err", err, "namespace", id.Namespace)
			}
		}()
	}
	if s.lineContent == nil || s.store == nil || s.vision == nil {
		s.logger.Warn("dispatcher: image dependencies missing", "namespace", id.Namespace)
		finalIntent = "image_dependencies_missing"
		return s.reply(ctx, id, replyToken, "รับรูปแล้วครับ")
	}
	opCtx, cancel := s.operationContext(ctx)
	defer cancel()
	data, contentType, err := s.lineContent.GetMessageContent(opCtx, messageID)
	if err != nil {
		s.logger.Error("dispatcher: image download failed", "err", err, "namespace", id.Namespace)
		finalIntent = "image_download_failed"
		if isTimeoutErr(err) {
			return s.replyTimeoutNotice(id, replyToken)
		}
		return s.reply(ctx, id, replyToken, "รับรูปไม่สำเร็จครับ กรุณาส่งรูปใหม่อีกครั้ง")
	}
	s.logger.Info("dispatcher: image downloaded", "namespace", id.Namespace, "messageId", messageID, "contentType", contentType, "bytes", len(data))
	_, _ = s.memory.SaveConversationMessage(ctx, memory.SaveMessageInput{
		OwnerID: id.UserID, AgentID: id.AgentID, Namespace: id.Namespace,
		Role: "user", Content: "[image]", Metadata: s.withSessionMetadata(ctx, map[string]any{"source": "line", "contentType": contentType, "size": len(data)}),
	})

	objectName := fmt.Sprintf("%s/%s/%s%s", id.UserID, thaitime.Now().Format("2006-01-02"), uuid.NewString(), extFromContentType(contentType))
	bucket, err := s.store.Put(opCtx, objectName, data, contentType)
	if err != nil {
		s.logger.Error("dispatcher: image store failed", "err", err, "namespace", id.Namespace)
		finalIntent = "image_store_failed"
		if isTimeoutErr(err) {
			return s.replyTimeoutNotice(id, replyToken)
		}
		return s.reply(ctx, id, replyToken, "รับรูปแล้วครับ แต่เก็บรูปไม่สำเร็จ กรุณาส่งใหม่อีกครั้ง")
	}
	s.logger.Info("dispatcher: image stored", "namespace", id.Namespace, "bucket", bucket, "objectName", objectName, "bytes", len(data))
	img, err := s.memory.SaveStoredImage(ctx, memory.SaveStoredImageInput{
		OwnerID: id.UserID, AgentID: id.AgentID, Namespace: id.Namespace,
		LineMessageID: messageID, Bucket: bucket, ObjectName: objectName, ContentType: contentType, Size: len(data),
	})
	if err != nil {
		s.logger.Error("dispatcher: image metadata failed", "err", err, "namespace", id.Namespace)
	} else {
		s.logger.Info("dispatcher: image metadata saved", "namespace", id.Namespace, "storedImageId", img.ID.Hex())
	}

	vision, err := s.vision.AnalyzeImage(opCtx, id.Namespace, thClawsSessionID(ctx), data, contentType)
	if err != nil {
		s.logger.Error("dispatcher: vision failed", "err", err, "namespace", id.Namespace)
		finalIntent = "vision_failed"
		if isTimeoutErr(err) {
			return s.replyTimeoutNotice(id, replyToken)
		}
		return s.reply(ctx, id, replyToken, "เก็บรูปไว้แล้วครับ แต่ AI อ่านรูปไม่สำเร็จ ลองส่งใหม่อีกครั้งนะครับ")
	}
	s.logger.Info("dispatcher: vision completed",
		"namespace", id.Namespace,
		"isSlip", vision.IsSlip,
		"hasAmount", vision.HasAmount,
		"amount", vision.Amount,
		"directionHint", vision.DirectionHint,
		"confidence", vision.Confidence,
		"replyLength", len([]rune(vision.FinalReply)),
	)
	reply := vision.FinalReply
	if vision.IsSlip && img != nil {
		slip, err := s.memory.CreatePendingSlip(ctx, id.UserID, id.AgentID, id.Namespace, img.ID)
		if err == nil {
			s.logger.Info("dispatcher: pending slip created", "namespace", id.Namespace, "slipId", slip.ID.Hex())
			var amount *float64
			if vision.HasAmount {
				v := vision.Amount
				amount = &v
			}
			direction, applyErr := s.memory.ApplyVisionToSlip(ctx, memory.SlipVisionInput{
				OwnerID: id.UserID, SlipID: slip.ID, IsSlip: vision.IsSlip, Amount: amount,
				FromAccountMasked: vision.FromAccountMasked, ToAccountMasked: vision.ToAccountMasked,
				FinalReply: vision.FinalReply, DirectionHint: vision.DirectionHint, Confidence: vision.Confidence,
			})
			if applyErr == nil {
				s.logger.Info("dispatcher: slip vision applied", "namespace", id.Namespace, "slipId", slip.ID.Hex(), "direction", direction)
				reply = buildSlipReply(vision.FinalReply, direction)
				finalIntent = "slip_vision"
				if vision.HasAmount {
					if err := s.saveSlipPendingAccounting(ctx, id, messageID, vision, direction); err != nil {
						s.logger.Warn("dispatcher: save slip pending accounting failed", "err", err, "namespace", id.Namespace, "slipId", slip.ID.Hex())
					}
					return s.replyChoicesWithLead(ctx, id, replyToken,
						[]string{reply},
						"จะให้ลงบัญชีรายการนี้ยังไงครับ?",
						[]string{"รายจ่าย", "รายรับ", "โอนเงิน", "ไม่บันทึก"})
				}
			} else {
				s.logger.Warn("dispatcher: slip vision apply failed", "err", applyErr, "namespace", id.Namespace, "slipId", slip.ID.Hex())
			}
		} else {
			s.logger.Warn("dispatcher: pending slip create failed", "err", err, "namespace", id.Namespace)
		}
	}
	if finalIntent == "image_received" {
		finalIntent = "image_vision"
	}
	return s.reply(ctx, id, replyToken, reply)
}

func (s *Service) replyLatestImage(ctx context.Context, id users.Identity, replyToken string) error {
	img, err := s.memory.GetLatestStoredImage(ctx, id.Namespace)
	if err != nil {
		s.logger.Error("dispatcher: latest image lookup failed", "err", err, "namespace", id.Namespace)
		return s.reply(ctx, id, replyToken, "ดึงรูปล่าสุดไม่สำเร็จครับ ลองใหม่อีกที")
	}
	if img == nil {
		return s.reply(ctx, id, replyToken, "ยังไม่พบรูปที่คุณเคยส่งไว้ครับ")
	}
	imageURL, err := media.BuildImageURL(s.publicBaseURL, img.ID.Hex(), s.imageAccessSecret, 10*time.Minute)
	if err != nil {
		s.logger.Error("dispatcher: latest image sign failed", "err", err, "namespace", id.Namespace, "storedImageId", img.ID.Hex())
		return s.reply(ctx, id, replyToken, "เปิดรูปล่าสุดยังไม่ได้ครับ ระบบลิงก์รูปยังไม่พร้อม")
	}
	return s.replyImage(ctx, id, replyToken, "รูปนี้ครับ รูปล่าสุดที่คุณส่งไว้", imageURL, img.ID.Hex())
}

func (s *Service) analyzeLatestImage(ctx context.Context, id users.Identity, replyToken string) error {
	if s.store == nil || s.vision == nil {
		return s.reply(ctx, id, replyToken, "ตอนนี้ระบบอ่านรูปยังไม่พร้อมครับ")
	}
	img, err := s.memory.GetLatestStoredImage(ctx, id.Namespace)
	if err != nil {
		s.logger.Error("dispatcher: latest image lookup for analysis failed", "err", err, "namespace", id.Namespace)
		return s.reply(ctx, id, replyToken, "ดึงรูปล่าสุดไม่สำเร็จครับ ลองใหม่อีกที")
	}
	if img == nil {
		return s.reply(ctx, id, replyToken, "ยังไม่พบรูปที่คุณเคยส่งไว้ครับ")
	}
	opCtx, cancel := s.operationContext(ctx)
	defer cancel()
	data, contentType, err := s.store.Get(opCtx, img.Bucket, img.ObjectName)
	if err != nil {
		s.logger.Error("dispatcher: latest image object load failed", "err", err, "namespace", id.Namespace, "storedImageId", img.ID.Hex())
		if isTimeoutErr(err) {
			return s.replyTimeoutNotice(id, replyToken)
		}
		return s.reply(ctx, id, replyToken, "เปิดรูปล่าสุดจากคลังภาพไม่สำเร็จครับ")
	}
	vision, err := s.vision.AnalyzeImage(opCtx, id.Namespace, thClawsSessionID(ctx), data, chooseContentType(contentType, img.ContentType))
	if err != nil {
		s.logger.Error("dispatcher: latest image vision failed", "err", err, "namespace", id.Namespace, "storedImageId", img.ID.Hex())
		if isTimeoutErr(err) {
			return s.replyTimeoutNotice(id, replyToken)
		}
		return s.reply(ctx, id, replyToken, "AI อ่านรูปล่าสุดไม่สำเร็จครับ ลองส่งรูปใหม่อีกครั้งนะครับ")
	}
	reply := vision.FinalReply
	if vision.IsSlip {
		reply = buildSlipReply(vision.FinalReply, vision.DirectionHint)
		if vision.HasAmount {
			if err := s.saveSlipPendingAccounting(ctx, id, img.ID.Hex(), vision, vision.DirectionHint); err != nil {
				s.logger.Warn("dispatcher: save latest slip pending accounting failed", "err", err, "namespace", id.Namespace, "storedImageId", img.ID.Hex())
			}
			return s.replyChoicesWithLead(ctx, id, replyToken,
				[]string{reply},
				"จะให้ลงบัญชีรายการนี้ยังไงครับ?",
				[]string{"รายจ่าย", "รายรับ", "โอนเงิน", "ไม่บันทึก"})
		}
	}
	return s.reply(ctx, id, replyToken, reply)
}

func (s *Service) saveSlipPendingAccounting(ctx context.Context, id users.Identity, causationID string, vision parser.VisionResult, direction string) error {
	note := strings.TrimSpace(vision.FinalReply)
	if note == "" {
		note = "สลิปโอนเงิน"
	}
	category := "อื่น ๆ"
	counterpartyName := ""
	counterpartyRole := ""
	switch direction {
	case "INCOME":
		category = "รายรับอื่น ๆ"
		counterpartyName = vision.FromName
		counterpartyRole = "from"
	case "EXPENSE":
		category = "อื่น ๆ"
		counterpartyName = vision.ToName
		counterpartyRole = "to"
	default:
		direction = ""
	}
	return s.memory.SavePendingAccounting(ctx, memory.PendingAccountingInput{
		OwnerID:          id.UserID,
		AgentID:          id.AgentID,
		Namespace:        id.Namespace,
		Original:         note,
		Direction:        direction,
		Amount:           vision.Amount,
		Note:             "สลิปโอนเงิน",
		Category:         category,
		CounterpartyName: counterpartyName,
		CounterpartyRole: counterpartyRole,
		Confidence:       vision.Confidence,
		CausationID:      causationID,
	})
}

func (s *Service) replyImage(ctx context.Context, id users.Identity, replyToken, text, imageURL, imageID string) error {
	s.logger.Info("dispatcher: image reply sending", "userId", id.UserID, "namespace", id.Namespace, "storedImageId", imageID)
	if err := s.replier.ReplyImage(ctx, replyToken, text, imageURL); err != nil {
		s.logger.Error("dispatcher: image reply failed", "err", err, "userId", id.UserID, "namespace", id.Namespace)
		return err
	}
	if _, err := s.memory.SaveConversationMessage(ctx, memory.SaveMessageInput{
		OwnerID: id.UserID, AgentID: id.AgentID, Namespace: id.Namespace,
		Role: "assistant", Content: text + " [image]", Metadata: s.withSessionMetadata(ctx, map[string]any{"source": "line_reply", "storedImageId": imageID}),
	}); err != nil {
		s.logger.Warn("dispatcher: image reply conversation save failed", "err", err, "namespace", id.Namespace)
	}
	if err := s.memory.SaveEventLog(ctx, memory.EventLogInput{
		OwnerID: id.UserID, AgentID: id.AgentID, Namespace: id.Namespace,
		EventType: "line.image_reply.sent", Content: text,
		Metadata: s.withSessionMetadata(ctx, map[string]any{"storedImageId": imageID}),
	}); err != nil {
		s.logger.Warn("dispatcher: image reply event log save failed", "err", err, "namespace", id.Namespace)
	}
	return nil
}

func (s *Service) reply(ctx context.Context, id users.Identity, replyToken, text string) error {
	s.logger.Info("dispatcher: reply sending", "userId", id.UserID, "namespace", id.Namespace, "textLength", len([]rune(text)), "hasReplyToken", replyToken != "")
	if err := s.replier.ReplyText(ctx, replyToken, text); err != nil {
		s.logger.Error("dispatcher: reply failed", "err", err, "userId", id.UserID, "namespace", id.Namespace)
		return err
	}
	s.logger.Info("dispatcher: reply sent", "userId", id.UserID, "namespace", id.Namespace, "textLength", len([]rune(text)))
	if msgID, err := s.memory.SaveConversationMessage(ctx, memory.SaveMessageInput{
		OwnerID: id.UserID, AgentID: id.AgentID, Namespace: id.Namespace,
		Role: "assistant", Content: text, Metadata: s.withSessionMetadata(ctx, map[string]any{"source": "line_reply"}),
	}); err != nil {
		s.logger.Warn("dispatcher: assistant conversation save failed", "err", err, "namespace", id.Namespace)
	} else {
		s.logger.Info("dispatcher: assistant conversation saved", "mongoId", msgID, "namespace", id.Namespace)
	}
	if err := s.memory.SaveEventLog(ctx, memory.EventLogInput{
		OwnerID: id.UserID, AgentID: id.AgentID, Namespace: id.Namespace,
		EventType: "line.reply.sent", Content: text,
		Metadata: s.withSessionMetadata(ctx, nil),
	}); err != nil {
		s.logger.Warn("dispatcher: reply event log save failed", "err", err, "namespace", id.Namespace)
	} else {
		s.logger.Info("dispatcher: reply event log saved", "eventType", "line.reply.sent", "namespace", id.Namespace)
	}
	return nil
}

func (s *Service) replyAccountingCard(ctx context.Context, id users.Identity, replyToken, direction, note string, amount float64, category string, projects []ledger.ProjectSummary) error {
	occurredAt := thaitime.ShortDateTime(thaitime.Now())
	projectText := projectReplyText(projects)
	text := fmt.Sprintf("บันทึก%s %s %s บาท (%s) เรียบร้อยครับ", direction, note, ledger.FormatMoney(amount), category)
	if projectText != "" {
		text += "\nโครงการ: " + projectText
	}
	s.logger.Info("dispatcher: accounting card sending",
		"userId", id.UserID, "namespace", id.Namespace, "direction", direction, "amount", amount, "projectCount", len(projects), "hasReplyToken", replyToken != "")
	if err := s.replier.ReplyAccountingCard(ctx, replyToken, direction, note, amount, category, occurredAt, projectText); err != nil {
		s.logger.Error("dispatcher: accounting card failed", "err", err, "userId", id.UserID, "namespace", id.Namespace)
		return s.reply(ctx, id, replyToken, text)
	}
	if _, err := s.memory.SaveConversationMessage(ctx, memory.SaveMessageInput{
		OwnerID: id.UserID, AgentID: id.AgentID, Namespace: id.Namespace,
		Role: "assistant", Content: text, Metadata: s.withSessionMetadata(ctx, map[string]any{"source": "line_reply", "messageKind": "accounting_card"}),
	}); err != nil {
		s.logger.Warn("dispatcher: accounting card conversation save failed", "err", err, "namespace", id.Namespace)
	}
	if err := s.memory.SaveEventLog(ctx, memory.EventLogInput{
		OwnerID: id.UserID, AgentID: id.AgentID, Namespace: id.Namespace,
		EventType: "line.accounting_card.sent", Content: text,
		Metadata: s.withSessionMetadata(ctx, map[string]any{"direction": direction, "amount": amount, "category": category, "projects": projectNames(projects)}),
	}); err != nil {
		s.logger.Warn("dispatcher: accounting card event log save failed", "err", err, "namespace", id.Namespace)
	}
	return nil
}

func (s *Service) resolveProjectAllocations(ctx context.Context, id users.Identity, text string, amount float64) ([]ledger.AllocationInput, []ledger.ProjectSummary) {
	if s.writer == nil || amount <= 0 {
		return nil, nil
	}
	allocations, projects, err := s.writer.ResolveProjectAllocations(ctx, id.UserID, id.Namespace, text, amount)
	if err != nil {
		s.logger.Warn("dispatcher: resolve project allocations failed", "err", err, "namespace", id.Namespace)
		return nil, nil
	}
	if len(projects) > 0 {
		s.logger.Info("dispatcher: project allocations resolved",
			"namespace", id.Namespace,
			"projectCount", len(projects),
			"allocationCount", len(allocations),
			"amount", amount,
		)
	}
	return allocations, projects
}

func projectReplyText(projects []ledger.ProjectSummary) string {
	names := projectNames(projects)
	if len(names) == 0 {
		return ""
	}
	return strings.Join(names, ", ")
}

func projectNames(projects []ledger.ProjectSummary) []string {
	names := []string{}
	seen := map[string]bool{}
	for _, p := range projects {
		name := strings.TrimSpace(p.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

func (s *Service) replySummaryCard(ctx context.Context, id users.Identity, replyToken, title, body string) error {
	s.logger.Info("dispatcher: summary card sending", "userId", id.UserID, "namespace", id.Namespace, "title", title, "textLength", len([]rune(body)), "hasReplyToken", replyToken != "")
	if err := s.replier.ReplySummaryCard(ctx, replyToken, title, body); err != nil {
		s.logger.Error("dispatcher: summary card failed", "err", err, "userId", id.UserID, "namespace", id.Namespace)
		return s.reply(ctx, id, replyToken, body)
	}
	if _, err := s.memory.SaveConversationMessage(ctx, memory.SaveMessageInput{
		OwnerID: id.UserID, AgentID: id.AgentID, Namespace: id.Namespace,
		Role: "assistant", Content: body, Metadata: s.withSessionMetadata(ctx, map[string]any{"source": "line_reply", "messageKind": "summary_card", "title": title}),
	}); err != nil {
		s.logger.Warn("dispatcher: summary card conversation save failed", "err", err, "namespace", id.Namespace)
	}
	if err := s.memory.SaveEventLog(ctx, memory.EventLogInput{
		OwnerID: id.UserID, AgentID: id.AgentID, Namespace: id.Namespace,
		EventType: "line.summary_card.sent", Content: body,
		Metadata: s.withSessionMetadata(ctx, map[string]any{"title": title}),
	}); err != nil {
		s.logger.Warn("dispatcher: summary card event log save failed", "err", err, "namespace", id.Namespace)
	}
	return nil
}

func (s *Service) replyOpenMiniApp(ctx context.Context, id users.Identity, replyToken string) error {
	if s.liffIssuer == nil {
		return s.reply(ctx, id, replyToken, "Mini App ยังไม่พร้อมเปิดครับ ลองใหม่อีกครั้งนะครับ")
	}
	token, err := s.liffIssuer.IssueToken(id)
	if err != nil {
		s.logger.Error("dispatcher: liff token issue failed", "err", err, "namespace", id.Namespace)
		return s.reply(ctx, id, replyToken, "เปิดสมุดบัญชีไม่สำเร็จครับ ลองใหม่อีกครั้งนะครับ")
	}
	appURL := s.liffOpenURL(token)
	s.logger.Info("dispatcher: liff card sending", "userId", id.UserID, "namespace", id.Namespace, "hasReplyToken", replyToken != "")
	if err := s.replier.ReplyOpenAppCard(ctx, replyToken, appURL); err != nil {
		s.logger.Error("dispatcher: liff card failed", "err", err, "namespace", id.Namespace)
		return err
	}
	text := "ส่งปุ่มเปิด Mini App บัญชีครัวเรือนแล้วครับ"
	if _, err := s.memory.SaveConversationMessage(ctx, memory.SaveMessageInput{
		OwnerID: id.UserID, AgentID: id.AgentID, Namespace: id.Namespace,
		Role: "assistant", Content: text, Metadata: s.withSessionMetadata(ctx, map[string]any{"source": "line_reply", "messageKind": "liff_card"}),
	}); err != nil {
		s.logger.Warn("dispatcher: liff card conversation save failed", "err", err, "namespace", id.Namespace)
	}
	if err := s.memory.SaveEventLog(ctx, memory.EventLogInput{
		OwnerID: id.UserID, AgentID: id.AgentID, Namespace: id.Namespace,
		EventType: "line.liff_card.sent", Content: text,
		Metadata: s.withSessionMetadata(ctx, map[string]any{"urlPath": "/liff/dashboard"}),
	}); err != nil {
		s.logger.Warn("dispatcher: liff card event log save failed", "err", err, "namespace", id.Namespace)
	}
	return nil
}

func (s *Service) liffOpenURL(token string) string {
	encoded := url.QueryEscape(token)
	if strings.TrimSpace(s.publicBaseURL) != "" {
		return strings.TrimRight(s.publicBaseURL, "/") + "/liff/dashboard?t=" + encoded
	}
	if s.liffID != "" {
		return "https://liff.line.me/" + url.PathEscape(s.liffID) + "?t=" + encoded
	}
	return "/liff/dashboard?t=" + encoded
}

func (s *Service) handlePendingAccountingChoice(ctx context.Context, id users.Identity, replyToken, messageID, choice string) (bool, error) {
	pending, err := s.memory.GetPendingAccounting(ctx, id.Namespace)
	if err != nil {
		s.logger.Warn("dispatcher: get pending accounting failed", "err", err, "namespace", id.Namespace)
		return false, nil
	}
	if pending == nil {
		return false, nil
	}
	s.logger.Info("dispatcher: pending accounting choice received",
		"namespace", id.Namespace,
		"choice", choice,
		"amount", pending.Amount,
		"hasAmount", pending.Amount > 0,
	)

	if choice == "ignore" || choice == "transfer" {
		if err := s.memory.ClearPendingAccounting(ctx, id.Namespace); err != nil {
			s.logger.Warn("dispatcher: clear pending accounting failed", "err", err, "namespace", id.Namespace)
		}
		if choice == "transfer" {
			_ = s.memory.SaveEventLog(ctx, memory.EventLogInput{
				OwnerID:   id.UserID,
				AgentID:   id.AgentID,
				Namespace: id.Namespace,
				EventType: "ledger.transfer_not_recorded",
				Content:   pending.Original,
				Metadata: s.withSessionMetadata(ctx, map[string]any{
					"amount":           pending.Amount,
					"note":             pending.Note,
					"counterpartyName": pending.CounterpartyName,
					"counterpartyRole": pending.CounterpartyRole,
					"reason":           "user_confirmed_transfer",
				}),
			})
			return true, s.reply(ctx, id, replyToken, "โอเคครับ ถือว่าโอนเฉย ๆ ไม่บันทึกบัญชีให้ครับ")
		}
		return true, s.reply(ctx, id, replyToken, "รับทราบครับ ไม่บันทึกรายการนี้ให้ครับ")
	}

	if pending.Amount <= 0 {
		direction := ""
		if choice == "income" {
			direction = "INCOME"
		}
		if choice == "expense" {
			direction = "EXPENSE"
		}
		if err := s.memory.SavePendingAccounting(ctx, memory.PendingAccountingInput{
			OwnerID: id.UserID, AgentID: id.AgentID, Namespace: id.Namespace,
			Original: pending.Original, Direction: direction, Note: pending.Note, Category: pending.Category,
			CounterpartyName: pending.CounterpartyName, CounterpartyRole: pending.CounterpartyRole,
			Confidence: pending.Confidence, CausationID: pending.CausationID,
		}); err != nil {
			s.logger.Warn("dispatcher: save pending direction failed", "err", err, "namespace", id.Namespace)
		}
		return true, s.replyChoices(ctx, id, replyToken,
			"ได้ครับ แต่ยังขาดจำนวนเงินครับ บอกเลขมาอีกนิด เช่น 200 บาท เดี๋ยวเลขาจัดให้แบบไม่เดา",
			[]string{"ไม่บันทึก"})
	}

	txType := "EXPENSE"
	direction := "รายจ่าย"
	category := "อื่น ๆ"
	if choice == "income" {
		txType = "INCOME"
		direction = "รายรับ"
		category = "รายรับอื่น ๆ"
	}
	if pending.Category != "" && pending.Category != "อื่น ๆ" && pending.Category != "รายรับอื่น ๆ" {
		category = pending.Category
	}
	allocations, projects := s.resolveProjectAllocations(ctx, id, pending.Original+" "+pending.Note, pending.Amount)
	txID, err := s.writer.CreatePersonalTransaction(ctx, ledger.WriteTxInput{
		UserID:           id.UserID,
		AgentID:          id.AgentID,
		Namespace:        id.Namespace,
		Type:             txType,
		Amount:           pending.Amount,
		Category:         category,
		Note:             pending.Note,
		CounterpartyName: pending.CounterpartyName,
		CounterpartyRole: pending.CounterpartyRole,
		Confidence:       0.85,
		CausationID:      messageID,
		Allocations:      allocations,
	})
	if err != nil {
		s.logger.Error("dispatcher: pending ledger write failed", "err", err, "namespace", id.Namespace)
		return true, s.reply(ctx, id, replyToken, "บันทึกไม่สำเร็จครับ ลองอีกทีนะ เลขาขอแก้มือ")
	}
	if err := s.memory.ClearPendingAccounting(ctx, id.Namespace); err != nil {
		s.logger.Warn("dispatcher: clear pending after record failed", "err", err, "namespace", id.Namespace)
	}
	s.logger.Info("dispatcher: pending accounting stored", "txId", txID, "namespace", id.Namespace, "type", txType, "amount", pending.Amount, "projectCount", len(projects))
	return true, s.replyAccountingCard(ctx, id, replyToken, direction, pending.Note, pending.Amount, category, projects)
}

func (s *Service) handlePendingAmount(ctx context.Context, id users.Identity, replyToken, messageID, text string) (bool, error) {
	pending, err := s.memory.GetPendingAccounting(ctx, id.Namespace)
	if err != nil {
		s.logger.Warn("dispatcher: get pending amount failed", "err", err, "namespace", id.Namespace)
		return false, nil
	}
	if pending == nil || pending.Amount > 0 || pending.Direction == "" {
		return false, nil
	}
	parsed := parser.Parse(text)
	if parsed.Intent != parser.IntentRecordExpense && parsed.Intent != parser.IntentRecordIncome {
		return true, s.replyChoices(ctx, id, replyToken,
			"ยังไม่เจอจำนวนเงินครับ ขอเป็นเลขหรือคำจำนวนเงิน เช่น 200 บาท / สองร้อย",
			[]string{"ไม่บันทึก"})
	}
	txType := pending.Direction
	direction := "รายจ่าย"
	category := "อื่น ๆ"
	if txType == "INCOME" {
		direction = "รายรับ"
		category = "รายรับอื่น ๆ"
	}
	if pending.Category != "" && pending.Category != "อื่น ๆ" && pending.Category != "รายรับอื่น ๆ" {
		category = pending.Category
	}
	note := pending.Note
	if strings.TrimSpace(note) == "" {
		note = pending.Original
	}
	allocations, projects := s.resolveProjectAllocations(ctx, id, pending.Original+" "+note, parsed.Amount)
	txID, err := s.writer.CreatePersonalTransaction(ctx, ledger.WriteTxInput{
		UserID:           id.UserID,
		AgentID:          id.AgentID,
		Namespace:        id.Namespace,
		Type:             txType,
		Amount:           parsed.Amount,
		Category:         category,
		Note:             note,
		CounterpartyName: pending.CounterpartyName,
		CounterpartyRole: pending.CounterpartyRole,
		Confidence:       0.85,
		CausationID:      messageID,
		Allocations:      allocations,
	})
	if err != nil {
		s.logger.Error("dispatcher: pending amount ledger write failed", "err", err, "namespace", id.Namespace)
		return true, s.reply(ctx, id, replyToken, "บันทึกไม่สำเร็จครับ ลองอีกทีนะ เลขาขอแก้มือ")
	}
	if err := s.memory.ClearPendingAccounting(ctx, id.Namespace); err != nil {
		s.logger.Warn("dispatcher: clear pending after amount failed", "err", err, "namespace", id.Namespace)
	}
	s.logger.Info("dispatcher: pending amount stored", "txId", txID, "namespace", id.Namespace, "type", txType, "amount", parsed.Amount, "projectCount", len(projects))
	return true, s.replyAccountingCard(ctx, id, replyToken, direction, note, parsed.Amount, category, projects)
}

func (s *Service) replyChoices(ctx context.Context, id users.Identity, replyToken, text string, choices []string) error {
	return s.replyChoicesWithLead(ctx, id, replyToken, nil, text, choices)
}

func (s *Service) replyChoicesWithLead(ctx context.Context, id users.Identity, replyToken string, leadTexts []string, text string, choices []string) error {
	s.logger.Info("dispatcher: quick reply sending",
		"userId", id.UserID,
		"namespace", id.Namespace,
		"textLength", len([]rune(text)),
		"leadCount", len(leadTexts),
		"choices", len(choices),
		"hasReplyToken", replyToken != "",
	)
	if err := s.replier.ReplyTextsWithChoices(ctx, replyToken, leadTexts, text, choices); err != nil {
		s.logger.Error("dispatcher: quick reply failed", "err", err, "userId", id.UserID, "namespace", id.Namespace)
		return err
	}
	s.logger.Info("dispatcher: quick reply sent", "userId", id.UserID, "namespace", id.Namespace, "choices", len(choices))
	if _, err := s.memory.SaveConversationMessage(ctx, memory.SaveMessageInput{
		OwnerID: id.UserID, AgentID: id.AgentID, Namespace: id.Namespace,
		Role: "assistant", Content: text, Metadata: s.withSessionMetadata(ctx, map[string]any{"source": "line_reply", "quickReplyChoices": choices}),
	}); err != nil {
		s.logger.Warn("dispatcher: quick reply conversation save failed", "err", err, "namespace", id.Namespace)
	}
	if err := s.memory.SaveEventLog(ctx, memory.EventLogInput{
		OwnerID: id.UserID, AgentID: id.AgentID, Namespace: id.Namespace,
		EventType: "line.quick_reply.sent", Content: text,
		Metadata: s.withSessionMetadata(ctx, map[string]any{"choiceCount": len(choices)}),
	}); err != nil {
		s.logger.Warn("dispatcher: quick reply event log save failed", "err", err, "namespace", id.Namespace)
	}
	return nil
}

func (s *Service) enqueueMemoryJobs(ctx context.Context, id users.Identity, text string) {
	if s.jobs == nil {
		s.logger.Warn("dispatcher: memory jobs skipped because queue client is nil", "namespace", id.Namespace)
		return
	}
	extractJob := queue.BrainExtractJob{
		OwnerID: id.UserID, AgentID: id.AgentID, Namespace: id.Namespace, Text: text,
		CausationID: uuid.NewString(),
	}
	if err := s.jobs.EnqueueBrainExtract(ctx, extractJob); err != nil {
		s.logger.Warn("dispatcher: brain extract enqueue failed", "err", err, "namespace", id.Namespace)
	} else {
		s.logger.Info("dispatcher: brain extract enqueue requested", "namespace", id.Namespace, "causationId", extractJob.CausationID)
	}
	compactJob := queue.MemoryCompactJob{
		OwnerID: id.UserID, AgentID: id.AgentID, Namespace: id.Namespace,
	}
	if err := s.jobs.EnqueueMemoryCompact(ctx, compactJob); err != nil {
		s.logger.Warn("dispatcher: memory compact enqueue failed", "err", err, "namespace", id.Namespace)
	} else {
		s.logger.Info("dispatcher: memory compact enqueue requested", "namespace", id.Namespace)
	}
}

func (s *Service) searchWebIfNeeded(ctx context.Context, id users.Identity, text string) ([]websearch.Result, error) {
	if s.webSearch == nil || !websearch.ShouldSearch(text) {
		return nil, nil
	}
	s.logger.Info("dispatcher: thclaws web search requested", "namespace", id.Namespace, "textLength", len([]rune(text)))
	searchCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	results, err := s.webSearch.Search(searchCtx, text, 3)
	if err != nil {
		s.logger.Warn("dispatcher: thclaws web search failed", "err", err, "namespace", id.Namespace)
		_ = s.memory.SaveEventLog(ctx, memory.EventLogInput{
			OwnerID:   id.UserID,
			AgentID:   id.AgentID,
			Namespace: id.Namespace,
			EventType: "thclaws.web_search.failed",
			Source:    "thclaws",
			Content:   text,
			Metadata:  s.withSessionMetadata(ctx, map[string]any{"reason": err.Error()}),
		})
		return nil, err
	}
	urls := make([]string, 0, len(results))
	for _, result := range results {
		urls = append(urls, result.URL)
	}
	s.logger.Info("dispatcher: thclaws web search completed", "namespace", id.Namespace, "results", len(results))
	_ = s.memory.SaveEventLog(ctx, memory.EventLogInput{
		OwnerID:   id.UserID,
		AgentID:   id.AgentID,
		Namespace: id.Namespace,
		EventType: "thclaws.web_search.completed",
		Source:    "thclaws",
		Content:   text,
		Metadata:  s.withSessionMetadata(ctx, map[string]any{"resultCount": len(results), "urls": urls}),
	})
	return results, nil
}

func (s *Service) replyBudgetExceeded(ctx context.Context) bool {
	if s.replyTimeout <= 0 {
		return false
	}
	receivedAt, ok := queue.ReceivedAt(ctx)
	if !ok {
		return false
	}
	return time.Since(receivedAt) >= s.replyTimeout
}

func (s *Service) operationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if s.replyTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	receivedAt, ok := queue.ReceivedAt(ctx)
	if !ok {
		return context.WithCancel(ctx)
	}
	remaining := s.replyTimeout - time.Since(receivedAt) - 2*time.Second
	if remaining <= 0 {
		remaining = time.Millisecond
	}
	if remaining > 25*time.Second {
		remaining = 25 * time.Second
	}
	return context.WithTimeout(ctx, remaining)
}

func (s *Service) replyTimeoutNotice(id users.Identity, replyToken string) error {
	s.logger.Warn("dispatcher: replying timeout notice", "userId", id.UserID, "namespace", id.Namespace)
	replyCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.reply(replyCtx, id, replyToken, timeoutReplyText)
}

func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func (s *Service) buildPrompt(ctx context.Context, id users.Identity, text string, webResults []websearch.Result, webErr error) string {
	namespace := id.Namespace
	res, err := s.memory.GetContext(ctx, namespace)
	if err != nil {
		s.logger.Warn("dispatcher: context fetch failed", "err", err, "namespace", namespace)
		return text
	}
	s.logger.Info("dispatcher: context loaded",
		"namespace", namespace,
		"recentHistory", len(res.RecentHistory),
		"hasCompactSummary", res.CompactSummary != "",
		"wikiPages", len(res.WikiPages),
	)
	var b strings.Builder
	if info := thClawsSessionFromContext(ctx); info.ID != "" {
		b.WriteString("thClaws session broker:\n")
		b.WriteString("- sessionId: " + info.ID + "\n")
		b.WriteString("- runtime: " + info.Runtime + "\n")
		b.WriteString("- model: " + info.Model + "\n")
	}
	b.WriteString("เวลาปัจจุบันของระบบ: " + thaitime.ShortDateTime(thaitime.Now()) + "\n")
	b.WriteString("หน้าที่ของ thClaws: วิเคราะห์บริบท วางแผนคำตอบ/เครื่องมือแบบปลอดภัย แล้วสรุปตอบผู้ใช้\n")
	b.WriteString("skills เปิดใช้งาน:\n")
	b.WriteString("- นักบัญชีครัวเรือน: ช่วยจัดหมวดรายรับรายจ่าย ทำบัญชีครัวเรือนแบบแยกประเภท สรุปเงินสด กระแสเงินสด งบประมาณ เป้าหมาย และหนี้ โดยยึดรายการจริงเท่านั้น\n")
	b.WriteString("- นักการเงินส่วนบุคคล: ให้คำแนะนำการเงินแบบระมัดระวัง เช่น วางงบ ออมเงิน เงินสำรองฉุกเฉิน จัดลำดับหนี้ และเป้าหมายการเงิน ห้ามรับประกันผลตอบแทนหรือแนะนำลงทุนเฉพาะตัวถ้าข้อมูลไม่พอ\n")
	b.WriteString("วิเคราะห์ประวัติการคุยก่อนตอบ เหมือนเลขาส่วนตัวที่จำบริบทได้ดี คุยเป็นธรรมชาติ ใช้คำว่า 'ครับ' เท่านั้น ไม่ใช้ 'ครับ/ค่ะ'\n")
	b.WriteString("ใช้ข้อมูลจริงจาก history, memory summary, wiki ส่วนตัวของ namespace นี้เท่านั้น ห้ามแต่งเรื่องหรือเดาข้ามผู้ใช้\n")
	b.WriteString("ตัวเลขบัญชี ยอดเงิน รายการรับจ่าย และรูปภาพ ต้องมาจาก tool/ledger/image result จริงเท่านั้น ถ้าไม่มีผลลัพธ์จริงให้บอกว่าไม่พบข้อมูล\n")
	b.WriteString("ถ้าความมั่นใจต่ำหรือบริบทไม่พอ ให้ตั้งคำถามกลับผู้ใช้ทันที ไม่ต้องฝืนตอบ และถามซ้ำได้จนมั่นใจ\n")
	b.WriteString("ถ้าคำถามอ้างถึงเรื่องก่อนหน้าแต่ข้อมูลไม่พอ ให้ถามสั้น ๆ เพื่อยืนยันก่อน ห้ามเดาว่าใช่\n")
	b.WriteString("ถ้าเป็นเรื่องส่วนตัว/ยอดเงิน/ประวัติที่ไม่มีใน history หรือ tool result ให้บอกว่าไม่มีข้อมูลพอแล้วถามต่อ\n")
	b.WriteString("ถ้าเป็นคำถามความรู้ทั่วไป สูตรอาหาร วิธีทำ แผน หรือคำแนะนำทั่วไป ให้ตอบได้ตามปกติ แต่ต้องบอกชัดว่าเป็นคำแนะนำทั่วไป ไม่ใช่ข้อมูลส่วนตัวจากฐานข้อมูล\n")
	b.WriteString("ถ้าเป็นคำถามเชิงความเห็นเกี่ยวกับคน/นิสัย ให้ตอบแบบระมัดระวัง เช่น 'จากที่เห็นในประวัติ...' และห้ามฟันธงเกินหลักฐาน\n")
	if isFinancialAdviceRequest(text) {
		b.WriteString("financial_context_from_ledger:\n")
		if snapshot := s.financialSnapshot(ctx, id); snapshot != "" {
			b.WriteString(snapshot)
		} else {
			b.WriteString("- ยังดึงตัวเลขบัญชีจริงไม่ได้หรือยังไม่มีรายการพอ ให้ถามข้อมูลเพิ่มก่อนแนะนำแบบเจาะจง\n")
		}
		b.WriteString("กติกาคำแนะนำการเงิน: เริ่มจากตัวเลขจริงที่มี ถ้าขาดรายได้ประจำ หนี้ เป้าหมาย หรือภาระครอบครัว ให้ถามเพิ่ม ห้ามแต่งตัวเลขเอง\n")
	}
	b.WriteString("ถ้ามี internet_search ให้ใช้ข้อมูลจากรายการนั้นเป็นหลักสำหรับข้อมูลสด และแนบชื่อแหล่งที่มาหรือ URL สั้น ๆ\n")
	b.WriteString("ถ้าผู้ใช้ถามเรื่องข้อมูลสดแต่ internet_search_error เกิดขึ้น ให้บอกว่าค้นเว็บไม่ได้ตอนนี้และถามว่าจะให้ลองใหม่ไหม\n")
	b.WriteString("ห้ามบอกว่าบันทึกหรือทำรายการแล้ว ถ้ายังไม่มีผลจาก tool/ledger จริงใน prompt\n")
	if len(webResults) > 0 {
		b.WriteString("internet_search:\n")
		for i, r := range webResults {
			b.WriteString(fmt.Sprintf("%d. %s\nurl: %s\nsnippet: %s\n", i+1, r.Title, r.URL, r.Snippet))
		}
	} else if webErr != nil {
		b.WriteString("internet_search_error: ค้นเว็บไม่สำเร็จในรอบนี้\n")
	}
	if res.CompactSummary != "" {
		b.WriteString("สรุปเดิม:\n" + res.CompactSummary + "\n")
	}
	if len(res.WikiPages) > 0 {
		b.WriteString("wiki:\n")
		for _, p := range res.WikiPages {
			b.WriteString("- " + p.Title + ": " + p.Summary + "\n")
		}
	}
	if len(res.RecentHistory) > 0 {
		b.WriteString("history:\n")
		for _, m := range res.RecentHistory {
			b.WriteString(m.Role + ": " + m.Content + "\n")
		}
	}
	b.WriteString("คำถามล่าสุด: " + text)
	return b.String()
}

func buildSlipReply(summary, direction string) string {
	directionText := "ยังไม่แน่ใจว่าเป็นรายรับหรือรายจ่าย"
	switch direction {
	case "INCOME":
		directionText = "น่าจะเป็นรายรับ"
	case "EXPENSE":
		directionText = "น่าจะเป็นรายจ่าย"
	case "TRANSFER":
		directionText = "น่าจะเป็นการโอนระหว่างบัญชี"
	}
	return summary + "\n" + directionText + " รบกวนยืนยันก่อนบันทึกบัญชีครับ"
}

func thaiToday() string {
	return thaitime.TodaySentence(thaitime.Now())
}

func helpText() string {
	return "ผมช่วยเป็นเลขาส่วนตัวผ่าน LINE ได้ครับ\n" +
		"- บันทึกบัญชี: กาแฟ 60 / ค่าไฟ 980 / เงินเดือน 25000\n" +
		"- อ่านสลิป/รูป: ส่งรูปมาได้เลย ถ้าไม่ชัวร์ผมจะถามก่อนบันทึก\n" +
		"- สรุปเงิน: สรุปวันนี้ / สรุปเดือนนี้ / ตอนนี้เหลือเงินเท่าไหร่\n" +
		"- Mini App: พิมพ์ เปิดสมุดบัญชี เพื่อดูกราฟ รายการล่าสุด และบัญชีแยกประเภท\n" +
		"- นักบัญชีครัวเรือน: ช่วยดูรับ-จ่าย งบประมาณ หมวดค่าใช้จ่าย และเงินคงเหลือ\n" +
		"- นักการเงิน: ช่วยวางแผนออมเงิน จัดลำดับหนี้ เงินสำรอง และเป้าหมายการเงินแบบไม่เดาตัวเลข\n" +
		"- ถามทั่วไป: สูตรอาหาร วางแผนงาน ค้นข้อมูล หรือให้ช่วยคิดก็ได้ครับ"
}

func (s *Service) financialSnapshot(ctx context.Context, id users.Identity) string {
	if s.summary == nil {
		return ""
	}
	var b strings.Builder
	if balance, err := s.summary.Balance(ctx, id.UserID, id.Namespace); err == nil && strings.TrimSpace(balance) != "" {
		b.WriteString("- ภาพรวม: " + balance + "\n")
	} else if err != nil {
		s.logger.Warn("dispatcher: financial snapshot balance failed", "err", err, "namespace", id.Namespace)
	}
	if month, err := s.summary.ThisMonth(ctx, id.UserID, id.Namespace); err == nil && strings.TrimSpace(month) != "" {
		b.WriteString("- เดือนนี้:\n" + month + "\n")
	} else if err != nil {
		s.logger.Warn("dispatcher: financial snapshot month failed", "err", err, "namespace", id.Namespace)
	}
	if today, err := s.summary.Today(ctx, id.UserID, id.Namespace); err == nil && strings.TrimSpace(today) != "" {
		b.WriteString("- วันนี้:\n" + today + "\n")
	} else if err != nil {
		s.logger.Warn("dispatcher: financial snapshot today failed", "err", err, "namespace", id.Namespace)
	}
	return b.String()
}

func wantsImageRecall(text string) bool {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return false
	}
	return (strings.Contains(clean, "ดูรูป") || strings.Contains(clean, "ส่งรูป") || strings.Contains(clean, "รูปล่าสุด")) &&
		(strings.Contains(clean, "เมื่อกี้") || strings.Contains(clean, "ล่าสุด") || strings.Contains(clean, "เก็บ") || strings.Contains(clean, "ไว้"))
}

func wantsLatestImageAnalysis(text string) bool {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return false
	}
	if strings.Contains(clean, "อ่านรูป") || strings.Contains(clean, "วิเคราะห์รูป") || strings.Contains(clean, "ดูสลิป") || strings.Contains(clean, "อ่านสลิป") {
		return true
	}
	return strings.Contains(clean, "รูปเมื่อกี้") && (strings.Contains(clean, "คืออะไร") || strings.Contains(clean, "อะไร"))
}

func wantsMiniApp(text string) bool {
	clean := strings.ToLower(strings.TrimSpace(text))
	if clean == "" {
		return false
	}
	phrases := []string{
		"เปิดสมุดบัญชี", "สมุดบัญชี", "mini app", "มินิแอป", "liff", "dashboard", "แดชบอร์ด",
		"เปิดบัญชี", "หน้าบัญชี", "กราฟ", "รายงานบัญชี",
	}
	for _, phrase := range phrases {
		if strings.Contains(clean, phrase) {
			return true
		}
	}
	return false
}

func chooseContentType(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" && primary != "application/octet-stream" {
		return primary
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	return "application/octet-stream"
}

func accountingChoices(text string) []string {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return []string{"รายจ่าย", "รายรับ", "โอนเฉยๆ", "ไม่บันทึก"}
	}
	return []string{
		"รายจ่าย " + clean,
		"รายรับ " + clean,
		"โอนเฉยๆ",
		"ไม่บันทึก",
	}
}

func isCapabilityQuestion(text string) bool {
	clean := strings.ToLower(strings.TrimSpace(text))
	if clean == "" {
		return false
	}
	phrases := []string{
		"ทำอะไรได้บ้าง", "ทำไรได้บ้าง", "มีอะไรบ้าง", "ใช้ทำอะไร", "ทำอะไรได้",
		"service", "บริการ", "ช่วยอะไรได้", "ช่วยอะไรได้บ้าง",
	}
	for _, phrase := range phrases {
		if strings.Contains(clean, phrase) {
			return true
		}
	}
	return false
}

func isFrustration(text string) bool {
	clean := strings.ToLower(strings.TrimSpace(text))
	if clean == "" {
		return false
	}
	phrases := []string{
		"กาก", "ห่วย", "โง่", "ไม่ตรง", "ตอบมั่ว", "มั่ว", "ไม่ได้เรื่อง", "ใช้ไม่ได้",
	}
	for _, phrase := range phrases {
		if strings.Contains(clean, phrase) {
			return true
		}
	}
	return false
}

func asksAssistantToGuess(text string) bool {
	clean := strings.ToLower(strings.TrimSpace(text))
	return strings.Contains(clean, "คิดเอา") || strings.Contains(clean, "เดาเอา") || strings.Contains(clean, "จัดการเอง")
}

func accountingChoice(text string) string {
	switch strings.TrimSpace(text) {
	case "รายรับ", "บันทึกรายรับ", "เงินเข้า":
		return "income"
	case "รายจ่าย", "บันทึกรายจ่าย", "ค่าใช้จ่าย":
		return "expense"
	case "โอนเงิน", "โอนเฉยๆ", "โอนเฉย ๆ", "ไม่ใช่รายรับรายจ่าย":
		return "transfer"
	case "ไม่บันทึก", "ยกเลิก":
		return "ignore"
	default:
		return ""
	}
}

func isAmbiguousFollowUp(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	ambiguous := map[string]bool{
		"ใช่": true, "ไม่ใช่": true, "เอา": true, "ไม่เอา": true,
		"ตกลง": true, "โอเค": true, "ok": true, "ได้": true,
		"อันนั้น": true, "อันนี้": true, "เมื่อกี้": true,
	}
	if ambiguous[strings.ToLower(text)] {
		return true
	}
	if _, ok := parseAmountOnly(text); ok {
		return true
	}
	return false
}

func parseAmountOnly(text string) (float64, bool) {
	trimmed := strings.TrimSpace(strings.ReplaceAll(text, ",", ""))
	trimmed = strings.TrimSuffix(trimmed, "บาท")
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return 0, false
	}

	parsed := parser.Parse(trimmed)
	if parsed.Amount <= 0 {
		return 0, false
	}
	if fmt.Sprintf("%.0f", parsed.Amount) == trimmed {
		return parsed.Amount, true
	}

	if thaiAmountOnlyPattern.MatchString(trimmed) {
		return parsed.Amount, true
	}
	return 0, false
}

func pendingQuestion(note string, amount float64) string {
	if amount > 0 {
		return fmt.Sprintf("ผมเดาว่า \"%s\" %s บาท เป็นรายการบัญชี แต่ยังไม่ชัวร์ครับ ให้ลงฝั่งไหนดี?", note, ledger.FormatMoney(amount))
	}
	return "รายการนี้เหมือนเกี่ยวกับเงิน แต่เลขาขอเช็กก่อนครับ ให้ทำอะไรดี?"
}

func looksAccountingRelated(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if isKnowledgeOrAdviceRequest(text) {
		return false
	}
	clean := strings.ToLower(text)
	prefixes := []string{
		"ค่า", "ซื้อ", "จ่าย", "ใช้ไป", "เสีย", "รายรับ", "รายจ่าย", "รายได้",
		"เงินเดือน", "ขายของ", "ขายได้", "รับเงิน", "ได้เงิน", "ให้เงิน", "เมียให้", "แม่ให้", "พ่อให้",
		"โอน", "เติม", "บิล", "ค่าน้ำ", "ค่าไฟ", "น้ำมันรถ",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(clean, prefix) || strings.Contains(clean, " "+prefix) {
			return true
		}
	}
	return false
}

func isKnowledgeOrAdviceRequest(text string) bool {
	clean := strings.ToLower(strings.TrimSpace(text))
	if clean == "" {
		return false
	}
	phrases := []string{
		"สูตร", "วิธีทำ", "ทำยังไง", "ทำอย่างไร", "แนะนำ", "ควร", "วางแผน", "อธิบาย",
		"คืออะไร", "อะไรคือ", "ทำซูชิ", "หุงข้าว", "ทำอาหาร", "กินอะไรดี", "เก็บเงินยังไง", "ออมเงิน",
	}
	for _, phrase := range phrases {
		if strings.Contains(clean, phrase) {
			return true
		}
	}
	return false
}

func isFinancialAdviceRequest(text string) bool {
	clean := strings.ToLower(strings.TrimSpace(text))
	if clean == "" {
		return false
	}
	financeTerms := []string{
		"ออม", "เก็บเงิน", "วางแผนการเงิน", "วางแผนเงิน", "งบประมาณ", "ประหยัด",
		"หนี้", "ผ่อน", "เงินสำรอง", "ฉุกเฉิน", "ลงทุน", "รายรับรายจ่าย", "กระแสเงินสด",
		"บัญชีครัวเรือน", "ใช้เงิน", "บริหารเงิน", "คุมรายจ่าย", "เป้าหมายการเงิน",
	}
	adviceTerms := []string{"ควร", "แนะนำ", "วางแผน", "ทำยังไง", "ทำอย่างไร", "ช่วยดู", "ช่วยคิด"}
	hasFinance := false
	for _, term := range financeTerms {
		if strings.Contains(clean, term) {
			hasFinance = true
			break
		}
	}
	if !hasFinance {
		return false
	}
	for _, term := range adviceTerms {
		if strings.Contains(clean, term) {
			return true
		}
	}
	return strings.Contains(clean, "นักการเงิน") || strings.Contains(clean, "นักบัญชี")
}

func sanitizeChatAnswer(answer string) string {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "ขออภัยครับ รอบนี้ผมยังเรียบเรียงคำตอบไม่ได้ ลองถามใหม่อีกครั้งนะครับ"
	}
	replacements := map[string]string{
		"ครับ/ค่ะ":      "ครับ",
		"คะ/ค่ะ":        "ครับ",
		"นะครับ/ค่ะ":    "นะครับ",
		"ขอโทษครับ/ค่ะ": "ขอโทษครับ",
	}
	for old, repl := range replacements {
		answer = strings.ReplaceAll(answer, old, repl)
	}
	lower := strings.ToLower(answer)
	if strings.Contains(lower, "i'm sorry") || strings.Contains(lower, "i cannot provide") || strings.Contains(lower, "cannot provide a response") {
		return "ขอโทษครับ รอบนี้สมองกลางตอบไม่ตรงภาษาไทย ลองถามใหม่อีกครั้งนะครับ"
	}
	if strings.Contains(answer, "ผู้ช่วยอื่น") {
		return "ขอโทษครับ รอบก่อนตอบไม่ดี ผมจะตอบให้ตรงขึ้นและไม่เดาข้อมูลให้ครับ"
	}
	if claimsCompletedAction(answer) {
		return "ผมยังไม่ได้ทำรายการอะไรในรอบนี้นะครับ ถ้าต้องการบันทึกบัญชี ส่งรายการพร้อมจำนวนเงิน เช่น กาแฟ 60 ได้เลยครับ"
	}
	return answer
}

func claimsCompletedAction(answer string) bool {
	phrases := []string{
		"บันทึกแล้ว", "บันทึกเรียบร้อย", "ได้บันทึก", "ผมบันทึก", "ลงบัญชีแล้ว",
	}
	for _, phrase := range phrases {
		if strings.Contains(answer, phrase) {
			return true
		}
	}
	return false
}

func extFromContentType(contentType string) string {
	switch {
	case strings.Contains(contentType, "png"):
		return ".png"
	case strings.Contains(contentType, "webp"):
		return ".webp"
	default:
		ext := filepath.Ext(contentType)
		if ext == "" {
			return ".jpg"
		}
		return ext
	}
}

func redactID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// ── Household / Business intent detection ──

// isHouseholdCommand detects household recording commands like "บันทึกบ้าน", "ค่าใช้จ่ายบ้าน", etc.
func isHouseholdCommand(text string) bool {
	clean := strings.ToLower(strings.TrimSpace(text))
	if clean == "" {
		return false
	}
	phrases := []string{
		"บันทึกบ้าน", "ค่าใช้จ่ายบ้าน", "บันทึกให้บ้าน", "รายจ่ายบ้าน",
	}
	for _, phrase := range phrases {
		if strings.Contains(clean, phrase) {
			return true
		}
	}
	return false
}

// isHouseholdSummary detects household summary request like "สรุปบ้าน".
func isHouseholdSummary(text string) bool {
	clean := strings.ToLower(strings.TrimSpace(text))
	if clean == "" {
		return false
	}
	return strings.Contains(clean, "สรุปบ้าน")
}

// isBusinessSale detects business sale recording like "ขาย", "ยอดขาย", "รายได้ร้าน".
func isBusinessSale(text string) bool {
	clean := strings.ToLower(strings.TrimSpace(text))
	if clean == "" {
		return false
	}
	phrases := []string{"ขาย", "ยอดขาย", "รายได้ร้าน"}
	for _, phrase := range phrases {
		if strings.Contains(clean, phrase) {
			return true
		}
	}
	return false
}

// isBusinessPurchase detects business purchase recording like "ซื้อของเข้าร้าน", "ต้นทุนร้าน".
func isBusinessPurchase(text string) bool {
	clean := strings.ToLower(strings.TrimSpace(text))
	if clean == "" {
		return false
	}
	phrases := []string{"ซื้อของเข้าร้าน", "ต้นทุนร้าน", "ซื้อวัตถุดิบ"}
	for _, phrase := range phrases {
		if strings.Contains(clean, phrase) {
			return true
		}
	}
	return false
}

// isBusinessProfit detects business profit/summary like "กำไร", "สรุปร้าน", "สรุปธุรกิจ".
func isBusinessProfit(text string) bool {
	clean := strings.ToLower(strings.TrimSpace(text))
	if clean == "" {
		return false
	}
	phrases := []string{"กำไร", "สรุปร้าน", "สรุปธุรกิจ"}
	for _, phrase := range phrases {
		if strings.Contains(clean, phrase) {
			return true
		}
	}
	return false
}

// ── Household handler ──

// handleHouseholdText handles household transaction recording and summary from LINE chat.
func (s *Service) handleHouseholdText(ctx context.Context, id users.Identity, replyToken, messageID, text string) error {
	if s.writer == nil {
		return s.reply(ctx, id, replyToken, "ระบบบัญชีครัวเรือนยังไม่พร้อมครับ")
	}

	// If it's a summary request
	if isHouseholdSummary(text) {
		return s.handleHouseholdSummary(ctx, id, replyToken, text)
	}

	// Parse the text for amount / category / note using the existing parser
	parsed := parser.Parse(text)

	// If no amount found, ask the user
	if parsed.Amount <= 0 {
		return s.reply(ctx, id, replyToken,
			"รับทราบครับ อยากบันทึกบัญชีบ้าน แต่ยังไม่เห็นจำนวนเงินครับ ขอตัวเลขด้วยนะครับ เช่น บันทึกบ้าน ค่าไฟ 500")
	}

	// Query user's households
	households, err := s.writer.ListMyHouseholds(ctx, id.UserID)
	if err != nil {
		s.logger.Error("dispatcher: list households failed", "err", err, "userId", id.UserID)
		return s.reply(ctx, id, replyToken, "ดึงข้อมูลบ้านไม่สำเร็จครับ ลองอีกครั้งนะครับ")
	}

	if len(households) == 0 {
		return s.reply(ctx, id, replyToken,
			"คุณยังไม่มีบ้านในระบบครับ สร้างบ้านผ่าน Mini App ก่อน แล้วค่อยบันทึกบัญชีบ้านนะครับ")
	}

	// Auto-select if only 1 household; otherwise ask user to choose
	householdID := households[0].ID
	if len(households) > 1 {
		choices := make([]string, 0, len(households))
		for _, h := range households {
			choices = append(choices, h.Name)
		}
		// For simplicity, store pending and let user pick
		_ = s.memory.SavePendingAccounting(ctx, memory.PendingAccountingInput{
			OwnerID:   id.UserID,
			AgentID:   id.AgentID,
			Namespace: id.Namespace,
			Original:  text,
			Amount:    parsed.Amount,
			Note:      parsed.Note,
			Category:  parsed.Category,
		})
		return s.replyChoicesWithLead(ctx, id, replyToken,
			[]string{"คุณมีหลายบ้านครับ ต้องการบันทึกให้บ้านไหน?"},
			"เลือกบ้านที่ต้องการบันทึกครับ",
			choices)
	}

	// Determine type: default to EXPENSE (most household entries are expenses)
	txType := "EXPENSE"
	if strings.Contains(strings.ToLower(text), "รายรับ") || strings.Contains(strings.ToLower(text), "รายได้") {
		txType = "INCOME"
	}

	category := parsed.Category
	if category == "" {
		category = "อื่น ๆ"
	}
	note := parsed.Note
	if note == "" {
		note = strings.TrimSpace(text)
	}

	txID, err := s.writer.CreateHouseholdTransaction(ctx, id.UserID, householdID, ledger.HouseholdTransactionInput{
		Type:     txType,
		Amount:   parsed.Amount,
		Category: category,
		Note:     note,
	})
	if err != nil {
		s.logger.Error("dispatcher: household transaction failed", "err", err)
		return s.reply(ctx, id, replyToken, "บันทึกบัญชีบ้านไม่สำเร็จครับ ลองอีกครั้งนะครับ")
	}

	s.logger.Info("dispatcher: household transaction stored",
		"txId", txID, "householdId", householdID, "type", txType, "amount", parsed.Amount)

	direction := "รายจ่าย"
	if txType == "INCOME" {
		direction = "รายรับ"
	}
	return s.replyAccountingCard(ctx, id, replyToken, direction, note, parsed.Amount, category, nil)
}

// handleHouseholdSummary handles "สรุปบ้าน" requests.
func (s *Service) handleHouseholdSummary(ctx context.Context, id users.Identity, replyToken, text string) error {
	households, err := s.writer.ListMyHouseholds(ctx, id.UserID)
	if err != nil {
		s.logger.Error("dispatcher: list households for summary failed", "err", err)
		return s.reply(ctx, id, replyToken, "ดึงข้อมูลบ้านไม่สำเร็จครับ ลองอีกครั้งนะครับ")
	}

	if len(households) == 0 {
		return s.reply(ctx, id, replyToken,
			"คุณยังไม่มีบ้านในระบบครับ สร้างบ้านผ่าน Mini App ก่อนนะครับ")
	}

	// If multiple households, ask which one
	if len(households) > 1 {
		choices := make([]string, 0, len(households))
		for _, h := range households {
			choices = append(choices, h.Name)
		}
		return s.replyChoices(ctx, id, replyToken, "ต้องการสรุปบ้านไหนครับ?", choices)
	}

	return s.buildAndReplyHouseholdSummary(ctx, id, replyToken, households[0])
}

// buildAndReplyHouseholdSummary computes and replies with a household summary.
func (s *Service) buildAndReplyHouseholdSummary(ctx context.Context, id users.Identity, replyToken string, h ledger.Household) error {
	now := thaitime.Now()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	to := now

	summary, err := ledger.HouseholdSummary(ctx, s.writer, id.UserID, h.ID,
		from.Format(time.RFC3339Nano), to.Format(time.RFC3339Nano))
	if err != nil {
		s.logger.Error("dispatcher: household summary failed", "err", err)
		return s.reply(ctx, id, replyToken, "สรุปบัญชีบ้านไม่สำเร็จครับ ลองอีกครั้งนะครับ")
	}

	title := fmt.Sprintf("สรุปบ้าน: %s", h.Name)
	return s.replySummaryCard(ctx, id, replyToken, title, summary)
}

// ── Business handler ──

// handleBusinessText handles business transaction recording and profit summary from LINE chat.
func (s *Service) handleBusinessText(ctx context.Context, id users.Identity, replyToken, messageID, text string) error {
	if s.writer == nil {
		return s.reply(ctx, id, replyToken, "ระบบบัญชีธุรกิจยังไม่พร้อมครับ")
	}

	clean := strings.ToLower(strings.TrimSpace(text))

	// Business summary / profit
	if isBusinessProfit(text) {
		return s.handleBusinessProfit(ctx, id, replyToken)
	}

	// Query user's businesses
	businesses, err := s.writer.ListMyBusinesses(ctx, id.UserID)
	if err != nil {
		s.logger.Error("dispatcher: list businesses failed", "err", err)
		return s.reply(ctx, id, replyToken, "ดึงข้อมูลธุรกิจไม่สำเร็จครับ ลองอีกครั้งนะครับ")
	}

	if len(businesses) == 0 {
		return s.reply(ctx, id, replyToken,
			"คุณยังไม่มีธุรกิจในระบบครับ สร้างธุรกิจผ่าน Mini App ก่อนนะครับ")
	}

	// Auto-select if only 1 business
	businessID := businesses[0].ID
	if len(businesses) > 1 {
		choices := make([]string, 0, len(businesses))
		for _, b := range businesses {
			choices = append(choices, b.Name)
		}
		return s.replyChoices(ctx, id, replyToken, "คุณมีหลายธุรกิจครับ ต้องการบันทึกให้ธุรกิจไหน?", choices)
	}

	// Parse amount from text
	parsed := parser.Parse(text)

	if isBusinessSale(text) {
		// "ขาย [product] [amount]"
		if parsed.Amount <= 0 {
			return s.reply(ctx, id, replyToken,
				"รับทราบครับ อยากบันทึกยอดขาย แต่ยังไม่เห็นจำนวนเงินครับ ขอตัวเลขด้วยนะครับ เช่น ขาย กาแฟ 120")
		}
		product := parsed.Note
		if product == "" {
			product = "สินค้า"
		}
		txID, err := ledger.RecordBusinessSale(ctx, s.writer, id.UserID, businessID, product, parsed.Amount, 0, 0)
		if err != nil {
			s.logger.Error("dispatcher: business sale failed", "err", err)
			return s.reply(ctx, id, replyToken, "บันทึกยอดขายไม่สำเร็จครับ ลองอีกครั้งนะครับ")
		}
		s.logger.Info("dispatcher: business sale stored",
			"txId", txID, "businessId", businessID, "product", product, "amount", parsed.Amount)
		return s.replyAccountingCard(ctx, id, replyToken, "รายรับ", product, parsed.Amount, "รายได้จากการขาย", nil)
	}

	if isBusinessPurchase(text) {
		// "ซื้อ [item] [amount]"
		if parsed.Amount <= 0 {
			return s.reply(ctx, id, replyToken,
				"รับทราบครับ อยากบันทึกต้นทุน แต่ยังไม่เห็นจำนวนเงินครับ ขอตัวเลขด้วยนะครับ เช่น ซื้อวัตถุดิบ กาแฟ 500")
		}
		item := parsed.Note
		if item == "" {
			item = "วัตถุดิบ"
		}
		txID, err := ledger.RecordBusinessPurchase(ctx, s.writer, id.UserID, businessID, item, parsed.Amount, 0, 0)
		if err != nil {
			s.logger.Error("dispatcher: business purchase failed", "err", err)
			return s.reply(ctx, id, replyToken, "บันทึกต้นทุนไม่สำเร็จครับ ลองอีกครั้งนะครับ")
		}
		s.logger.Info("dispatcher: business purchase stored",
			"txId", txID, "businessId", businessID, "item", item, "amount", parsed.Amount)
		return s.replyAccountingCard(ctx, id, replyToken, "รายจ่าย", item, parsed.Amount, "ต้นทุนสินค้า", nil)
	}

	_ = clean // already consumed
	return s.reply(ctx, id, replyToken, "รับทราบครับ แต่ผมไม่แน่ใจว่าต้องการทำอะไรกับธุรกิจครับ")
}

// handleBusinessProfit handles "กำไร", "สรุปร้าน", "สรุปธุรกิจ" requests.
func (s *Service) handleBusinessProfit(ctx context.Context, id users.Identity, replyToken string) error {
	businesses, err := s.writer.ListMyBusinesses(ctx, id.UserID)
	if err != nil {
		s.logger.Error("dispatcher: list businesses for profit failed", "err", err)
		return s.reply(ctx, id, replyToken, "ดึงข้อมูลธุรกิจไม่สำเร็จครับ ลองอีกครั้งนะครับ")
	}

	if len(businesses) == 0 {
		return s.reply(ctx, id, replyToken,
			"คุณยังไม่มีธุรกิจในระบบครับ สร้างธุรกิจผ่าน Mini App ก่อนนะครับ")
	}

	if len(businesses) > 1 {
		choices := make([]string, 0, len(businesses))
		for _, b := range businesses {
			choices = append(choices, b.Name)
		}
		return s.replyChoices(ctx, id, replyToken, "ต้องการดูกำไรของธุรกิจไหนครับ?", choices)
	}

	return s.buildAndReplyBusinessProfit(ctx, id, replyToken, businesses[0])
}

// buildAndReplyBusinessProfit computes and replies with a business profit summary.
func (s *Service) buildAndReplyBusinessProfit(ctx context.Context, id users.Identity, replyToken string, b ledger.BusinessInfo) error {
	now := thaitime.Now()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	to := now

	profit, err := ledger.ComputeBusinessProfit(ctx, s.writer, b.ID,
		from.Format(time.RFC3339Nano), to.Format(time.RFC3339Nano))
	if err != nil {
		s.logger.Error("dispatcher: business profit failed", "err", err)
		return s.reply(ctx, id, replyToken, "คำนวณกำไรไม่สำเร็จครับ ลองอีกครั้งนะครับ")
	}

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("สรุปธุรกิจ: %s\n", b.Name))
	msg.WriteString(fmt.Sprintf("ยอดขาย: %s บาท\n", ledger.FormatMoney(profit.TotalSales)))
	msg.WriteString(fmt.Sprintf("ต้นทุนสินค้า: %s บาท\n", ledger.FormatMoney(profit.TotalPurchases)))
	msg.WriteString(fmt.Sprintf("ค่าใช้จ่ายรวม: %s บาท\n", ledger.FormatMoney(profit.TotalExpenses)))
	msg.WriteString(fmt.Sprintf("กำไรขั้นต้น: %s บาท\n", ledger.FormatMoney(profit.GrossProfit)))
	msg.WriteString(fmt.Sprintf("กำไรสุทธิ: %s บาท", ledger.FormatMoney(profit.NetProfit)))

	return s.replySummaryCard(ctx, id, replyToken, fmt.Sprintf("กำไร: %s", b.Name), msg.String())
}
