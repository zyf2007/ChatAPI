package common

import "time"

type CreateAppAPIKeyInput struct {
	ID             string
	UserID         string
	Name           string
	KeyHash        string
	KeyPrefix      string
	Scopes         []string
	ResourceLimits map[string]any
	ExpiresAt      *time.Time
}

type CreateModelAPIKeyInput struct {
	ID            string
	UserID        string
	Name          string
	KeyCiphertext string
	KeyPrefix     string
	Model         string
}

type CreateUserInput struct {
	ID           string
	Username     string
	Email        string
	PasswordHash string
	Role         string
	IsActive     bool
	LocalAdmin   bool
}

type UpdateUserInput struct {
	ID           string
	Username     string
	Email        string
	PasswordHash string
	Role         string
	IsActive     bool
	LocalAdmin   bool
	LastLoginAt  *time.Time
}

type UpsertUserIdentityInput struct {
	ID            string
	UserID        string
	Provider      string
	Subject       string
	Email         string
	EmailVerified bool
	Profile       map[string]any
	LastLoginAt   *time.Time
}

type SetSystemConfigInput struct {
	Key   string
	Value map[string]any
}

type SetUserConfigInput struct {
	UserID string
	Key    string
	Value  map[string]any
}

type UpsertAuthVerificationCodeInput struct {
	Email          string
	Purpose        string
	CodeHash       string
	FailedAttempts int
	ExpiresAt      time.Time
	LastSentAt     time.Time
}

type UpsertAutomationRuleInput struct {
	ID      string
	UserID  string
	Enabled bool
	Payload map[string]any
}

type CreateAuditLogInput struct {
	ID           string
	ActorUserID  string
	ActorRole    string
	ActorSource  string
	EventType    string
	ResourceType string
	ResourceID   string
	Action       string
	Outcome      string
	IPAddress    string
	UserAgent    string
	Metadata     map[string]any
}

type ListAuditLogsInput struct {
	Limit       int
	EventType   string
	ActorUserID string
}

type CountAuditLogsInput struct {
	EventType    string
	ActorUserID  string
	ResourceType string
	Action       string
	Outcome      string
}

type CreateUploadedImageInput struct {
	ID               string
	OwnerID          string
	Filename         string
	OriginalFilename string
	ContentType      string
	Bytes            int64
	URL              string
}

type UpsertStorageFileDeletionFailureInput struct {
	Path      string
	Filename  string
	OwnerID   string
	Bytes     int64
	LastError string
}

type CreatePendingInput struct {
	ConversationID     string
	RequestID          string
	ResponseID         string
	OwnerID            string
	ReuseConversation  bool
	RequestFormat      string
	Model              string
	SystemContent      string
	DeveloperContent   string
	AssistantContent   string
	UserContent        string
	UserMessageContent string
	RequestMethod      string
	RequestPath        string
	RequestQuery       map[string][]string
	RequestHeaders     map[string][]string
	RequestBody        map[string]any
	// RawRequestBody is the immutable protocol fact captured at ingress.
	RawRequestBody map[string]any
	// RequestOptions is the normalized protocol snapshot derived from RawRequestBody.
	RequestOptions map[string]any
	// OptionChips is a UI projection cache. Do not treat it as protocol authority.
	OptionChips    []any
	ToolSchemas    []any
	BuiltinTools   []any
	ToolChoice     RequestToolChoice
	ResponseFormat RequestResponseFormat
	PreparedImages []CreatePendingImageAssetInput
}

type CreatePendingImageAssetInput struct {
	FileID            string
	Path              string
	MediaType         string
	Bytes             int64
	SHA256            string
	Width             int
	Height            int
	SourceKind        string
	OriginalName      string
	OriginalMediaType string
	InputPartIndex    int
}

type CreateMediaAssetInput struct {
	ID                string
	OwnerID           string
	FileID            string
	Path              string
	MediaType         string
	Bytes             int64
	SHA256            string
	Width             int
	Height            int
	SourceKind        string
	OriginalName      string
	OriginalMediaType string
	CreatedAt         time.Time
}

type CreateStagedMediaAssetInput struct {
	Asset          CreateMediaAssetInput
	ConversationID string
	RequestID      string
}

// OutputSegment is the durable ordered unit of assistant output.
// JSON tags are required: repository metadata is marshaled through encoding/json
// and reloaded as map[string]any, so exported field names alone are not stable.
type OutputSegment struct {
	Mode                string `json:"mode"`
	Text                string `json:"text"`
	ReasoningStreamMode string `json:"reasoning_stream_mode,omitempty"`
}

type CompletePendingInput struct {
	ConversationID      string
	ResponseID          string
	OutputText          string
	OutputSegments      []OutputSegment
	OutputPreview       string
	Mode                string
	ToolName            string
	ToolCallID          string
	ToolOutput          string
	ReasoningStreamMode string
	OutputPolicy        map[string]any
	OutputTokens        int
	FinishReason        string
	StopSequence        string
}

type UpdateDraftInput struct {
	ConversationID string
	DraftText      string
	OutputSegments []OutputSegment
}

type AbortPendingInput struct {
	ConversationID string
	Reason         string
}

type DisconnectPendingInput struct {
	ConversationID string
	Reason         string
}

type PendingTurnLifecycleMutationInput struct {
	ConversationID string
	Reason         string
	Identity       TurnIdentity
	EventID        string
	EventType      string
	EventLevel     string
	EventTitle     string
	EventDetail    string
	EventMetadata  map[string]any
	EventCreatedAt time.Time
}

type AppendConversationEventInput struct {
	ID             string
	ConversationID string
	OwnerID        string
	Type           string
	Level          string
	Title          string
	Detail         string
	RequestID      string
	Metadata       map[string]any
	CreatedAt      time.Time
}

type AppendConversationEventWithAssetInput struct {
	Event    AppendConversationEventInput
	AssetID  string
	AssetURL string
	Purpose  string
}

type DeleteConversationsResult struct {
	DeletedConversations     int                   `json:"deleted_conversations"`
	DeletedMessages          int                   `json:"deleted_messages"`
	DeletedAssetRefs         int                   `json:"deleted_asset_refs"`
	DeletedConversationItems []DeletedConversation `json:"deleted_conversation_items,omitempty"`
}

type DeletedConversation struct {
	ID      string `json:"id"`
	OwnerID string `json:"owner_id,omitempty"`
}

type DeleteUploadedImagesResult struct {
	DeletedImages int `json:"deleted_images"`
}

type ExpirePendingTurnsResult struct {
	ExpiredConversations int `json:"expired_conversations"`
}
