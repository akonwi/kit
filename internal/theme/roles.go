package theme

// Compatible semantic token names. Keeping the registry independent of a
// renderer prevents adapters from silently dropping established theme roles.
const (
	TokenBackground                 = "bg"
	TokenBackgroundSurface          = "bgSurface"
	TokenBackgroundMuted            = "bgMuted"
	TokenBackgroundAccent           = "bgAccent"
	TokenBackgroundTransparent      = "bgTransparent"
	TokenBorderDefault              = "borderDefault"
	TokenBorderFocused              = "borderFocused"
	TokenBorderAccent               = "borderAccent"
	TokenBorderDebug                = "borderDebug"
	TokenBorderStatus               = "borderStatus"
	TokenComposerBashBorder         = "composerBashBorder"
	TokenComposerBashExcludedBorder = "composerBashExcludedBorder"
	TokenTextPrimary                = "textPrimary"
	TokenTextSecondary              = "textSecondary"
	TokenTextMuted                  = "textMuted"
	TokenTextPlaceholder            = "textPlaceholder"
	TokenTextDebug                  = "textDebug"
	TokenUserText                   = "userText"
	TokenUserTextFocused            = "userTextFocused"
	TokenUserBorder                 = "userBorder"
	TokenAssistantText              = "assistantText"
	TokenToolText                   = "toolText"
	TokenReviewText                 = "reviewText"
	TokenErrorText                  = "errorText"
	TokenWarningText                = "warningText"
	TokenSubagentText               = "subagentText"
	TokenDebugLabel                 = "debugLabel"
	TokenMetaText                   = "metaText"
	TokenAttachmentText             = "attachmentText"
	TokenCursor                     = "cursor"
	TokenPickerBackground           = "pickerBg"
	TokenPickerBorder               = "pickerBorder"
	TokenPickerFocusedBackground    = "pickerFocusedBg"
	TokenPickerFocusedText          = "pickerFocusedText"
	TokenPickerItemText             = "pickerItemText"
	TokenPickerScrollThumb          = "pickerScrollThumb"
	TokenPickerScrollTrack          = "pickerScrollTrack"
	TokenScrollbarForeground        = "scrollbarFg"
	TokenScrollbarBackground        = "scrollbarBg"
	TokenPanelText                  = "panelText"
	TokenProgressNormal             = "progressNormal"
	TokenProgressWarning            = "progressWarning"
	TokenProgressCritical           = "progressCritical"
	TokenToggleOn                   = "toggleOn"
	TokenDiffAddedBackground        = "diffAddedBg"
	TokenDiffRemovedBackground      = "diffRemovedBg"
	TokenDiffAddedContentBackground = "diffAddedContentBg"
	TokenDiffRemovedContentBg       = "diffRemovedContentBg"
	TokenDiffAddedLineNumberBg      = "diffAddedLineNumberBg"
	TokenDiffRemovedLineNumberBg    = "diffRemovedLineNumberBg"
	TokenDiffCursorBackground       = "diffCursorBg"
	TokenDiffCursorGutterBackground = "diffCursorGutterBg"
	TokenDiffCursorAddedBackground  = "diffCursorAddedBg"
	TokenDiffCursorRemovedBg        = "diffCursorRemovedBg"
)

// Compatible syntax palette role names.
const (
	SyntaxText          = "text"
	SyntaxHeading       = "heading"
	SyntaxBold          = "bold"
	SyntaxItalic        = "italic"
	SyntaxLink          = "link"
	SyntaxList          = "list"
	SyntaxQuote         = "quote"
	SyntaxCodeInline    = "codeInline"
	SyntaxCodeBlock     = "codeBlock"
	SyntaxStrikethrough = "strikethrough"
	SyntaxConceal       = "conceal"
	SyntaxComment       = "comment"
	SyntaxString        = "string"
	SyntaxEscape        = "escape"
	SyntaxNumber        = "number"
	SyntaxKeyword       = "keyword"
	SyntaxKeywordType   = "keywordType"
	SyntaxFunction      = "function"
	SyntaxOperator      = "operator"
	SyntaxVariable      = "variable"
	SyntaxMember        = "member"
	SyntaxBuiltin       = "builtin"
	SyntaxType          = "type"
	SyntaxPunctuation   = "punctuation"
	SyntaxTag           = "tag"
	SyntaxTagAttribute  = "tagAttribute"
	SyntaxTagDelimiter  = "tagDelimiter"
	SyntaxAttribute     = "attribute"
	SyntaxLabel         = "label"
)

var tokenRoles = []string{
	TokenBackground, TokenBackgroundSurface, TokenBackgroundMuted, TokenBackgroundAccent,
	TokenBackgroundTransparent, TokenBorderDefault, TokenBorderFocused, TokenBorderAccent,
	TokenBorderDebug, TokenBorderStatus, TokenComposerBashBorder, TokenComposerBashExcludedBorder,
	TokenTextPrimary, TokenTextSecondary, TokenTextMuted, TokenTextPlaceholder, TokenTextDebug,
	TokenUserText, TokenUserTextFocused, TokenUserBorder, TokenAssistantText, TokenToolText,
	TokenReviewText, TokenErrorText, TokenWarningText, TokenSubagentText, TokenDebugLabel,
	TokenMetaText, TokenAttachmentText, TokenCursor, TokenPickerBackground, TokenPickerBorder,
	TokenPickerFocusedBackground, TokenPickerFocusedText, TokenPickerItemText, TokenPickerScrollThumb,
	TokenPickerScrollTrack, TokenScrollbarForeground, TokenScrollbarBackground, TokenPanelText,
	TokenProgressNormal, TokenProgressWarning, TokenProgressCritical, TokenToggleOn,
	TokenDiffAddedBackground, TokenDiffRemovedBackground, TokenDiffAddedContentBackground,
	TokenDiffRemovedContentBg, TokenDiffAddedLineNumberBg, TokenDiffRemovedLineNumberBg,
	TokenDiffCursorBackground, TokenDiffCursorGutterBackground, TokenDiffCursorAddedBackground,
	TokenDiffCursorRemovedBg,
}

var syntaxRoles = []string{
	SyntaxText, SyntaxHeading, SyntaxBold, SyntaxItalic, SyntaxLink, SyntaxList, SyntaxQuote,
	SyntaxCodeInline, SyntaxCodeBlock, SyntaxStrikethrough, SyntaxConceal, SyntaxComment,
	SyntaxString, SyntaxEscape, SyntaxNumber, SyntaxKeyword, SyntaxKeywordType, SyntaxFunction,
	SyntaxOperator, SyntaxVariable, SyntaxMember, SyntaxBuiltin, SyntaxType, SyntaxPunctuation,
	SyntaxTag, SyntaxTagAttribute, SyntaxTagDelimiter, SyntaxAttribute, SyntaxLabel,
}

var knownTokenRoles = roleSet(tokenRoles)
var knownSyntaxRoles = roleSet(syntaxRoles)

func roleSet(roles []string) map[string]struct{} {
	result := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		result[role] = struct{}{}
	}
	return result
}

// TokenRoles returns the compatible semantic token registry.
func TokenRoles() []string { return append([]string(nil), tokenRoles...) }

// SyntaxRoles returns the compatible syntax role registry.
func SyntaxRoles() []string { return append([]string(nil), syntaxRoles...) }

// IsKnownToken reports whether name is consumed by the compatible token model.
func IsKnownToken(name string) bool {
	_, ok := knownTokenRoles[name]
	return ok
}

// IsKnownSyntaxRole reports whether name is consumed by the compatible syntax model.
func IsKnownSyntaxRole(name string) bool {
	_, ok := knownSyntaxRoles[name]
	return ok
}
