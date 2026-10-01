package domain

import "time"

type DiagnosticResult struct {
	Output   string
	ExitCode int64
	Duration time.Duration
}

// AIProviderProfile is an OpenAI-compatible provider. Key* are persistence
// fields only and must never be serialized by an API response.
type AIProviderProfile struct {
	ID, Name, BaseURL, ChatPath, ModelsPath, DefaultModel string
	Enabled, Default                                      bool
	ContextSize                                           *int64
	MaxOutputTokens, TimeoutMS                            int64
	Temperature                                           float64
	InputPriceMicros, OutputPriceMicros                   *int64
	KeyCipher, KeyNonce                                   []byte
	KeyID                                                 *string
	CreatedAtMS, UpdatedAtMS                              int64
}

type AIConversation struct {
	ID, CreatorID, Title string
	BotID, SiteID        *string
	ProviderID, Model    *string
	CreatedAtMS          int64
	UpdatedAtMS          int64
}

type AICitation struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	RetrievedMS int64  `json:"retrieved_at_ms"`
}

type AIMessage struct {
	ID, ConversationID, Role, Content string
	CitationsJSON                     string
	CreatedAtMS                       int64
}

type AIRun struct {
	ID, ConversationID, UserID, Model, Mode, Status string
	ProviderID                                      *string
	LimitsJSON, PlanJSON                            string
	AutoApprovedAtMS                                *int64
	InputTokens, OutputTokens                       int64
	ErrorCode, ErrorMessage                         *string
	CreatedAtMS                                     int64
	StartedAtMS, FinishedAtMS                       *int64
}

type AIToolCall struct {
	ID, RunID, Name, ArgumentsJSON, Output string
	ProviderCallID                         *string // the provider's id, echoed only in the tool-result message
	CallIndex                              int64
	ApprovalState, Status                  string
	ExitCode, DurationMS                   *int64
	ErrorMessage                           *string
	CreatedAtMS                            int64
	FinishedAtMS                           *int64
}

type AIChangeSet struct {
	ID, RunID, TargetKind, TargetID, Status, Summary string
	CreatedAtMS                                      int64
	AppliedAtMS, RevertedAtMS                        *int64
	Files                                            []AIChangeFile
}

type AIChangeFile struct {
	ChangeSetID, Path, Operation, Diff string
	BeforeGzip, AfterGzip              []byte
	BeforeRevision, AfterRevision      *string
	Mode                               int64
}
