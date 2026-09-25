import SwiftUI

struct MCPStatusView: View {
    @Bindable var state: SessionStore
    let back: () -> Void
    @Environment(\.mica) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                VStack(alignment: .leading, spacing: 3) {
                    Text("MCP servers").font(.kit(size: 16, weight: .medium))
                    Text("Connections available to this session").font(.kit(size: 12)).foregroundStyle(theme.muted)
                }
                Spacer()
                Button { state.ui.palette = false } label: { Image(systemName: "xmark") }
                    .buttonStyle(.plain).keyboardShortcut(.cancelAction).accessibilityLabel("Close MCP servers")
            }.padding(20)
            Rule()
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    if servers.isEmpty {
                        Text("No MCP servers are configured.")
                            .foregroundStyle(theme.muted).frame(maxWidth: .infinity, alignment: .center).padding(.vertical, 72)
                    } else {
                        ForEach(servers) { server in serverCard(server) }
                    }
                    if !warnings.isEmpty {
                        VStack(alignment: .leading, spacing: 10) {
                            MetaLabel(text: "WARNINGS")
                            ForEach(Array(warnings.enumerated()), id: \.offset) { _, warning in
                                Label(warning, systemImage: "exclamationmark.triangle")
                                    .font(.kit(size: 12)).foregroundStyle(theme.warning)
                                    .fixedSize(horizontal: false, vertical: true).textSelection(.enabled)
                            }
                        }.padding(.top, 8)
                    }
                }.padding(20).frame(maxWidth: .infinity, alignment: .leading)
            }
            Rule()
            HStack { Button("Back", action: back); Spacer(); Button("Done") { state.ui.palette = false } }
                .padding(14)
        }
        .font(.kit(size: 13)).frame(width: 560, height: 520)
        .foregroundStyle(theme.text).background(theme.surface)
        .onExitCommand { state.ui.palette = false }
    }

    private var servers: [MCPServerStatus] { state.selected?.mcpServers ?? [] }
    private var warnings: [String] { state.selected?.mcpWarnings ?? [] }

    private func serverCard(_ server: MCPServerStatus) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                statusIndicator(server)
                Text(server.name).font(.kit(size: 13, weight: .medium)).textSelection(.enabled)
                Spacer()
                Text(server.state.capitalized).font(.kit(size: 11, weight: .medium)).foregroundStyle(statusColor(server.state))
            }
            if !server.description.isEmpty {
                Text(server.description).font(.kit(size: 12)).foregroundStyle(theme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
            HStack(spacing: 8) {
                chip(server.transport.uppercased())
                chip("\(server.toolCount) tool\(server.toolCount == 1 ? "" : "s")")
                if server.oauthSaved { chip("OAuth saved") }
            }
            if !server.configPath.isEmpty {
                HStack(alignment: .top, spacing: 8) {
                    Text(sourceLabel(server.source)).foregroundStyle(theme.muted).frame(width: 82, alignment: .leading)
                    Text(server.configPath).font(.kit(size: 11, design: .monospaced)).textSelection(.enabled)
                        .lineLimit(2).frame(maxWidth: .infinity, alignment: .leading)
                }.font(.kit(size: 11))
            }
            if !server.lastError.isEmpty {
                Label(server.lastError, systemImage: "exclamationmark.circle")
                    .font(.kit(size: 12)).foregroundStyle(theme.danger)
            }
        }
        .padding(14).background(theme.raised).clipShape(RoundedRectangle(cornerRadius: 8))
        .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(theme.border))
    }

    @ViewBuilder private func statusIndicator(_ server: MCPServerStatus) -> some View {
        if server.state == "connecting" || server.state == "authorizing" {
            ProgressView().controlSize(.small).frame(width: 14, height: 14)
        } else {
            Text(symbol(server.state)).font(.kit(size: 14, weight: .medium, design: .monospaced))
                .foregroundStyle(statusColor(server.state)).frame(width: 14)
        }
    }

    private func chip(_ text: String) -> some View {
        Text(text).font(.kit(size: 10, weight: .medium, design: .monospaced)).foregroundStyle(theme.muted)
            .padding(.horizontal, 7).padding(.vertical, 3).background(theme.hover).clipShape(RoundedRectangle(cornerRadius: 4))
    }

    private func symbol(_ state: String) -> String {
        switch state { case "connected": "✓"; case "disabled": "⊘"; case "error": "×"; default: "○" }
    }
    private func statusColor(_ state: String) -> Color {
        switch state { case "connected": theme.success; case "error": theme.danger; case "authorizing": theme.warning; default: theme.muted }
    }
    private func sourceLabel(_ source: String) -> String {
        switch source { case "kit-user": "User config"; case "shared-project": "Project"; case "kit-project": "Kit project"; default: source }
    }
}
