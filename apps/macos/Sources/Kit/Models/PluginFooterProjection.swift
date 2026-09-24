import Foundation

enum PluginFooterProjection {
    static let tokens: Set<String> = ["bg", "bgSurface", "bgMuted", "bgAccent", "bgTransparent", "borderDefault", "borderFocused", "borderAccent", "borderDebug", "borderStatus", "composerBashBorder", "composerBashExcludedBorder", "textPrimary", "textSecondary", "textMuted", "textPlaceholder", "textDebug", "userText", "userTextFocused", "userBorder", "assistantText", "toolText", "reviewText", "errorText", "warningText", "subagentText", "debugLabel", "metaText", "attachmentText", "cursor", "pickerBg", "pickerBorder", "pickerFocusedBg", "pickerFocusedText", "pickerItemText", "pickerScrollThumb", "pickerScrollTrack", "scrollbarFg", "scrollbarBg", "panelText", "progressNormal", "progressWarning", "progressCritical", "toggleOn", "diffAddedBg", "diffRemovedBg", "diffAddedContentBg", "diffRemovedContentBg", "diffAddedLineNumberBg", "diffRemovedLineNumberBg", "diffCursorBg", "diffCursorGutterBg", "diffCursorAddedBg", "diffCursorRemovedBg"]
    static func validate(_ footer: WirePluginFooter?) throws -> WirePluginFooter? {
        guard let footer else { return nil }
        guard (footer.items?.count ?? 0) <= 64 else { throw ClientError.invalidPayload }
        var seen = Set<String>()
        for item in footer.items ?? [] {
            guard PluginCommand.validSelection(id: item.id, instance: item.instance),
                  PluginCommand.matches(item.pluginId, "^[a-z][a-z0-9-]{0,31}$"),
                  item.pluginId != "kit", !item.pluginId.hasPrefix("kit-"),
                  item.id.hasPrefix(item.pluginId + "."), seen.insert(item.id).inserted,
                  let segments = item.content, !segments.isEmpty, segments.count <= 32 else { throw ClientError.invalidPayload }
            var size = 0
            for segment in segments {
                size += segment.text.utf8.count
                guard size <= 4096, PluginCommand.safeText(segment.text, limit: 4096) else { throw ClientError.invalidPayload }
                for token in [segment.style.fg, segment.style.bg].compactMap({ $0 }) where !token.isEmpty {
                    guard tokens.contains(token) else { throw ClientError.invalidPayload }
                }
            }
        }
        return footer
    }
}
