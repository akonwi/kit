package pluginprotocol

import "reflect"

// Direction says which side sends a method.
type Direction string

// Method directions.
const (
	PluginToKit Direction = "plugin-to-kit"
	KitToPlugin Direction = "kit-to-plugin"
	// Both sides send kit/cancel.
	Both Direction = "both"
)

// Method describes one protocol method.
type Method struct {
	// Name is the JSON-RPC method name.
	Name string
	// Title names the method in generated definitions, such as RegisterTool.
	Title     string
	Direction Direction
	// Notification methods have no id and no response.
	Notification bool
	// Params is the params type, or nil when the method has no params.
	Params reflect.Type
	// Result is the result type of a request; nil for a notification.
	Result reflect.Type
	// Errors lists the codes the receiver may answer a request with.
	Errors []ErrorCode
	// Summary is one sentence for the specification and generated docs.
	Summary string
}

func typeOf[T any]() reflect.Type { return reflect.TypeFor[T]() }

// Codes every plugin request may receive from Kit.
var pluginRequestErrors = []ErrorCode{CodeInvalidParams, CodeInternalError, CodeCancelled, CodeUnavailable, CodeTryLater}

func pluginRequestErrorsWith(extra ...ErrorCode) []ErrorCode {
	return append(append([]ErrorCode(nil), pluginRequestErrors...), extra...)
}

// Methods lists every version 2 method, in specification order.
func Methods() []Method {
	return []Method{
		{Name: "initialize", Title: "Initialize", Direction: KitToPlugin, Params: typeOf[InitializeParams](), Result: typeOf[InitializeResult](),
			Summary: "Opens the conversation; the plugin answers before sending anything else."},
		{Name: "shutdown", Title: "Shutdown", Direction: KitToPlugin, Result: nullType,
			Summary: "Asks the plugin to stop; it answers null and exits within two seconds."},
		{Name: "kit/cancel", Title: "Cancel", Direction: Both, Notification: true, Params: typeOf[CancelParams](),
			Summary: "Cancels a request the sender sent earlier."},

		{Name: "kit/commands/register", Title: "RegisterCommand", Direction: PluginToKit, Params: typeOf[CommandRegisterParams](), Result: typeOf[CanonicalIDResult](),
			Errors: pluginRequestErrorsWith(CodeConflict, CodeLimitExceeded), Summary: "Registers a command."},
		{Name: "kit/commands/unregister", Title: "UnregisterCommand", Direction: PluginToKit, Params: typeOf[LocalIDParams](), Result: nullType,
			Errors: pluginRequestErrorsWith(CodeLimitExceeded), Summary: "Removes a command; removing an unknown command succeeds."},
		{Name: "kit/commands/execute", Title: "ExecuteCommand", Direction: KitToPlugin, Params: typeOf[CommandExecuteParams](), Result: nullType,
			Errors: []ErrorCode{CodeFailed, CodeCancelled}, Summary: "Runs a command a user invoked."},

		{Name: "kit/tools/register", Title: "RegisterTool", Direction: PluginToKit, Params: typeOf[ToolRegisterParams](), Result: typeOf[ToolRegisterResult](),
			Errors: pluginRequestErrorsWith(CodeConflict, CodeLimitExceeded), Summary: "Registers a tool the model may call."},
		{Name: "kit/tools/unregister", Title: "UnregisterTool", Direction: PluginToKit, Params: typeOf[ToolIDParams](), Result: nullType,
			Errors: pluginRequestErrorsWith(CodeLimitExceeded), Summary: "Removes a tool; removing an unknown tool succeeds."},
		{Name: "kit/tools/execute", Title: "ExecuteTool", Direction: KitToPlugin, Params: typeOf[ToolExecuteParams](), Result: typeOf[ToolExecuteResult](),
			Errors: []ErrorCode{CodeFailed, CodeCancelled}, Summary: "Runs a tool the model called."},

		{Name: "kit/tool-calls/register-interceptor", Title: "RegisterInterceptor", Direction: PluginToKit, Result: nullType,
			Errors: pluginRequestErrorsWith(CodeLimitExceeded), Summary: "Asks Kit to consult the plugin before every tool call."},
		{Name: "kit/tool-calls/unregister-interceptor", Title: "UnregisterInterceptor", Direction: PluginToKit, Result: nullType,
			Errors: pluginRequestErrors, Summary: "Stops consulting the plugin before tool calls."},
		{Name: "kit/tool-calls/before-execute", Title: "BeforeExecute", Direction: KitToPlugin, Params: typeOf[BeforeExecuteParams](), Result: typeOf[ToolCallDecision](),
			Errors: []ErrorCode{CodeFailed, CodeCancelled}, Summary: "Asks an interceptor whether a tool call may run."},

		{Name: "kit/subagents/register", Title: "RegisterSubagent", Direction: PluginToKit, Params: typeOf[SubagentRegisterParams](), Result: typeOf[CanonicalIDResult](),
			Errors: pluginRequestErrorsWith(CodeConflict, CodeLimitExceeded), Summary: "Registers a subagent definition."},
		{Name: "kit/subagents/unregister", Title: "UnregisterSubagent", Direction: PluginToKit, Params: typeOf[LocalIDParams](), Result: nullType,
			Errors: pluginRequestErrorsWith(CodeLimitExceeded), Summary: "Removes a subagent definition."},

		{Name: "kit/footer/set", Title: "SetFooterItem", Direction: PluginToKit, Params: typeOf[FooterSetParams](), Result: typeOf[CanonicalIDResult](),
			Errors: pluginRequestErrorsWith(CodeLimitExceeded), Summary: "Creates or replaces a footer item."},
		{Name: "kit/footer/clear", Title: "ClearFooterItem", Direction: PluginToKit, Params: typeOf[LocalIDParams](), Result: nullType,
			Errors: pluginRequestErrorsWith(CodeLimitExceeded), Summary: "Removes a footer item."},
		{Name: "kit/footer/hide", Title: "HideFooterItem", Direction: PluginToKit, Params: typeOf[FooterTargetParams](), Result: nullType,
			Errors: pluginRequestErrorsWith(CodeLimitExceeded), Summary: "Hides a footer item while the claim lasts."},
		{Name: "kit/footer/show", Title: "ShowFooterItem", Direction: PluginToKit, Params: typeOf[FooterTargetParams](), Result: nullType,
			Errors: pluginRequestErrorsWith(CodeLimitExceeded), Summary: "Withdraws the plugin's hide claim."},

		{Name: "kit/ui/toast", Title: "Toast", Direction: PluginToKit, Notification: true, Params: typeOf[ToastParams](),
			Summary: "Shows a notification in the session's clients."},
		{Name: "kit/ui/confirm", Title: "Confirm", Direction: PluginToKit, Params: typeOf[ConfirmParams](), Result: typeOf[ConfirmResult](),
			Errors: pluginRequestErrorsWith(CodeFailed, CodeLimitExceeded), Summary: "Asks the user a yes-or-no question."},
		{Name: "kit/ui/input", Title: "Input", Direction: PluginToKit, Params: typeOf[InputParams](), Result: typeOf[InputResult](),
			Errors: pluginRequestErrorsWith(CodeFailed, CodeLimitExceeded), Summary: "Asks the user for text."},
		{Name: "kit/ui/select", Title: "Select", Direction: PluginToKit, Params: typeOf[SelectParams](), Result: typeOf[SelectResult](),
			Errors: pluginRequestErrorsWith(CodeFailed, CodeLimitExceeded), Summary: "Asks the user to choose an option."},

		{Name: "kit/session/submit-message", Title: "SubmitMessage", Direction: PluginToKit, Params: typeOf[SubmitMessageParams](), Result: typeOf[SubmitMessageResult](),
			Errors: pluginRequestErrorsWith(CodeConflict, CodeLimitExceeded), Summary: "Starts a turn in the plugin's idle session."},
		{Name: "kit/system/open-url", Title: "OpenURL", Direction: PluginToKit, Params: typeOf[OpenURLParams](), Result: nullType,
			Errors: pluginRequestErrorsWith(CodeLimitExceeded), Summary: "Asks the session's attached clients to open a web page."},

		{Name: "kit/events/project.changed", Title: "ProjectChanged", Direction: KitToPlugin, Notification: true, Params: typeOf[Project](),
			Summary: "The session's working directory changed; sent to user plugins only."},
		{Name: "kit/events/session.changed", Title: "SessionChanged", Direction: KitToPlugin, Notification: true, Params: typeOf[Session](),
			Summary: "The session was renamed."},
		{Name: "kit/events/git.changed", Title: "GitChanged", Direction: KitToPlugin, Notification: true, Params: typeOf[GitChangedParams](),
			Summary: "The working directory's Git state or pull request changed."},
		{Name: "kit/events/agent.turn.started", Title: "TurnStarted", Direction: KitToPlugin, Notification: true, Params: typeOf[TurnStartedParams](),
			Summary: "Kit admitted a turn."},
		{Name: "kit/events/agent.turn.completed", Title: "TurnCompleted", Direction: KitToPlugin, Notification: true, Params: typeOf[TurnCompletedParams](),
			Summary: "A turn settled; carries its user and assistant text."},
	}
}

// MethodNamed returns the method called name.
func MethodNamed(name string) (Method, bool) {
	for _, method := range Methods() {
		if method.Name == name {
			return method, true
		}
	}
	return Method{}, false
}

// Sends reports whether direction d's sender sends method m.
func (m Method) Sends(d Direction) bool { return m.Direction == d || m.Direction == Both }
