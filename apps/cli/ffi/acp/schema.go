package acp

import "encoding/json"

// ProtocolVersion is the ACP major version Kit speaks.
const ProtocolVersion = 1

// Method names of the ACP v1 methods Kit serves or calls.
const (
	methodInitialize         = "initialize"
	methodAuthenticate       = "authenticate"
	methodSessionNew         = "session/new"
	methodSessionLoad        = "session/load"
	methodSessionResume      = "session/resume"
	methodSessionList        = "session/list"
	methodSessionClose       = "session/close"
	methodSessionPrompt      = "session/prompt"
	methodSessionCancel      = "session/cancel"
	methodSetConfigOption    = "session/set_config_option"
	methodSessionUpdate      = "session/update"
	methodRequestPermission  = "session/request_permission"
	methodElicitationCreate  = "elicitation/create"
	methodCancelRequest      = "$/cancel_request"
	stopReasonEndTurn        = "end_turn"
	stopReasonCancelled      = "cancelled"
	permissionOutcomeChosen  = "selected"
	permissionOptionAllow    = "allow"
	permissionOptionReject   = "reject"
	elicitationActionAccept  = "accept"
	elicitationModeForm      = "form"
	configOptionModel        = "model"
	configOptionThinking     = "thinking"
	toolStatusPending        = "pending"
	toolStatusInProgress     = "in_progress"
	toolStatusCompleted      = "completed"
	toolStatusFailed         = "failed"
	compactionStatusRunning  = "in_progress"
	compactionStatusComplete = "completed"
	compactionStatusFailed   = "failed"
)

// Implementation names one side of the connection.
type Implementation struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

type initializeParams struct {
	ProtocolVersion    *int               `json:"protocolVersion"`
	ClientCapabilities clientCapabilities `json:"clientCapabilities"`
	ClientInfo         *Implementation    `json:"clientInfo"`
}

// clientCapabilities holds the client capabilities Kit reads. An omitted or
// null object means the client does not advertise the capability.
type clientCapabilities struct {
	Session     *clientSessionCapabilities `json:"session"`
	Elicitation *elicitationCapabilities   `json:"elicitation"`
}

type clientSessionCapabilities struct {
	Compaction json.RawMessage `json:"compaction"`
	Notices    json.RawMessage `json:"notices"`
}

type elicitationCapabilities struct {
	Form json.RawMessage `json:"form"`
}

// advertised reports whether a capability object is present and not null.
func advertised(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}

type initializeResult struct {
	ProtocolVersion   int               `json:"protocolVersion"`
	AgentCapabilities agentCapabilities `json:"agentCapabilities"`
	AgentInfo         Implementation    `json:"agentInfo"`
	AuthMethods       []struct{}        `json:"authMethods"`
}

type agentCapabilities struct {
	LoadSession         bool                `json:"loadSession"`
	PromptCapabilities  promptCapabilities  `json:"promptCapabilities"`
	MCPCapabilities     mcpCapabilities     `json:"mcpCapabilities"`
	SessionCapabilities sessionCapabilities `json:"sessionCapabilities"`
}

type promptCapabilities struct {
	Image           bool `json:"image"`
	Audio           bool `json:"audio"`
	EmbeddedContext bool `json:"embeddedContext"`
}

type mcpCapabilities struct {
	HTTP bool `json:"http"`
	SSE  bool `json:"sse"`
}

type empty struct{}

type sessionCapabilities struct {
	List   *empty `json:"list,omitempty"`
	Resume *empty `json:"resume,omitempty"`
	Close  *empty `json:"close,omitempty"`
}

// mcpServer is the part of a client's MCP server entry Kit reports. Kit
// never reads, logs, or forwards env values or headers.
type mcpServer struct {
	Name string `json:"name"`
}

type newSessionParams struct {
	CWD        string      `json:"cwd"`
	MCPServers []mcpServer `json:"mcpServers"`
}

type newSessionResult struct {
	SessionID     string         `json:"sessionId"`
	ConfigOptions []configOption `json:"configOptions"`
}

type loadSessionParams struct {
	SessionID  string      `json:"sessionId"`
	CWD        string      `json:"cwd"`
	MCPServers []mcpServer `json:"mcpServers"`
}

type loadSessionResult struct {
	ConfigOptions []configOption `json:"configOptions"`
}

type listSessionsParams struct {
	CWD    string `json:"cwd"`
	Cursor string `json:"cursor"`
}

type sessionInfo struct {
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	Title     string `json:"title,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type listSessionsResult struct {
	Sessions   []sessionInfo `json:"sessions"`
	NextCursor string        `json:"nextCursor,omitempty"`
}

type sessionIDParams struct {
	SessionID string `json:"sessionId"`
}

type promptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []contentBlock `json:"prompt"`
}

type promptResult struct {
	StopReason string `json:"stopReason"`
}

type setConfigOptionParams struct {
	SessionID string          `json:"sessionId"`
	ConfigID  string          `json:"configId"`
	Type      string          `json:"type"`
	Value     json.RawMessage `json:"value"`
}

type setConfigOptionResult struct {
	ConfigOptions []configOption `json:"configOptions"`
}

type cancelRequestParams struct {
	RequestID json.RawMessage `json:"requestId"`
}

// contentBlock is an ACP content block. Kit decodes the fields of every block
// type it accepts and writes text and image blocks.
type contentBlock struct {
	Type     string            `json:"type"`
	Text     string            `json:"text,omitempty"`
	Data     string            `json:"data,omitempty"`
	MimeType string            `json:"mimeType,omitempty"`
	URI      string            `json:"uri,omitempty"`
	Name     string            `json:"name,omitempty"`
	Resource *embeddedResource `json:"resource,omitempty"`
}

type embeddedResource struct {
	URI      string  `json:"uri"`
	MimeType string  `json:"mimeType,omitempty"`
	Text     *string `json:"text,omitempty"`
	Blob     *string `json:"blob,omitempty"`
}

func textBlock(text string) contentBlock { return contentBlock{Type: "text", Text: text} }

type sessionNotification struct {
	SessionID string `json:"sessionId"`
	Update    any    `json:"update"`
}

type contentChunk struct {
	SessionUpdate string       `json:"sessionUpdate"`
	Content       contentBlock `json:"content"`
	MessageID     string       `json:"messageId,omitempty"`
}

type toolCallLocation struct {
	Path string `json:"path"`
	Line *int   `json:"line,omitempty"`
}

// toolCallContent is one item of a tool call's content: "content" with a
// content block, or "diff" with a path and its old and new text.
type toolCallContent struct {
	Type    string        `json:"type"`
	Content *contentBlock `json:"content,omitempty"`
	Path    string        `json:"path,omitempty"`
	OldText *string       `json:"oldText,omitempty"`
	NewText *string       `json:"newText,omitempty"`
}

type toolCall struct {
	SessionUpdate string             `json:"sessionUpdate"`
	ToolCallID    string             `json:"toolCallId"`
	Title         string             `json:"title"`
	Name          string             `json:"name,omitempty"`
	Kind          string             `json:"kind"`
	Status        string             `json:"status"`
	Content       []toolCallContent  `json:"content,omitempty"`
	Locations     []toolCallLocation `json:"locations,omitempty"`
	RawInput      json.RawMessage    `json:"rawInput,omitempty"`
}

// toolCallUpdate patches a tool call; omitted fields keep their values.
type toolCallUpdate struct {
	SessionUpdate string             `json:"sessionUpdate,omitempty"`
	ToolCallID    string             `json:"toolCallId"`
	Title         string             `json:"title,omitempty"`
	Kind          string             `json:"kind,omitempty"`
	Status        string             `json:"status,omitempty"`
	Content       []toolCallContent  `json:"content,omitempty"`
	Locations     []toolCallLocation `json:"locations,omitempty"`
	RawInput      json.RawMessage    `json:"rawInput,omitempty"`
	RawOutput     json.RawMessage    `json:"rawOutput,omitempty"`
}

type availableCommand struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Input       *availableCommandInput `json:"input,omitempty"`
}

type availableCommandInput struct {
	Hint string `json:"hint"`
}

type availableCommandsUpdate struct {
	SessionUpdate     string             `json:"sessionUpdate"`
	AvailableCommands []availableCommand `json:"availableCommands"`
}

type configOption struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	Category     string `json:"category,omitempty"`
	Type         string `json:"type"`
	CurrentValue string `json:"currentValue"`
	Options      any    `json:"options"`
}

type configSelectOption struct {
	Value       string `json:"value"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type configSelectGroup struct {
	Group   string               `json:"group"`
	Name    string               `json:"name"`
	Options []configSelectOption `json:"options"`
}

type configOptionUpdate struct {
	SessionUpdate string         `json:"sessionUpdate"`
	ConfigOptions []configOption `json:"configOptions"`
}

type sessionInfoUpdate struct {
	SessionUpdate string `json:"sessionUpdate"`
	Title         string `json:"title"`
}

type usageCost struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

type usageUpdate struct {
	SessionUpdate string     `json:"sessionUpdate"`
	Used          int        `json:"used"`
	Size          int        `json:"size"`
	Cost          *usageCost `json:"cost,omitempty"`
}

type compactionUpdate struct {
	SessionUpdate string         `json:"sessionUpdate"`
	CompactionID  string         `json:"compactionId"`
	Status        string         `json:"status"`
	Summary       []contentBlock `json:"summary,omitempty"`
	Error         string         `json:"error,omitempty"`
}

type noticeUpdate struct {
	SessionUpdate string `json:"sessionUpdate"`
	Severity      string `json:"severity"`
	Title         string `json:"title"`
	Description   string `json:"description,omitempty"`
}

type permissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

type requestPermissionParams struct {
	SessionID string             `json:"sessionId"`
	ToolCall  toolCallUpdate     `json:"toolCall"`
	Options   []permissionOption `json:"options"`
}

type requestPermissionResult struct {
	Outcome struct {
		Outcome  string `json:"outcome"`
		OptionID string `json:"optionId"`
	} `json:"outcome"`
}

type elicitationParams struct {
	SessionID       string            `json:"sessionId"`
	ToolCallID      string            `json:"toolCallId,omitempty"`
	Mode            string            `json:"mode"`
	Message         string            `json:"message"`
	RequestedSchema elicitationSchema `json:"requestedSchema"`
}

type elicitationSchema struct {
	Type       string                         `json:"type"`
	Properties map[string]elicitationProperty `json:"properties"`
	Required   []string                       `json:"required,omitempty"`
}

type elicitationProperty struct {
	Type        string            `json:"type"`
	Title       string            `json:"title,omitempty"`
	Description string            `json:"description,omitempty"`
	Default     any               `json:"default,omitempty"`
	OneOf       []enumOption      `json:"oneOf,omitempty"`
	Items       *elicitationItems `json:"items,omitempty"`
}

type elicitationItems struct {
	AnyOf []enumOption `json:"anyOf"`
}

type enumOption struct {
	Const string `json:"const"`
	Title string `json:"title"`
}

type elicitationResult struct {
	Action  string                     `json:"action"`
	Content map[string]json.RawMessage `json:"content"`
}
