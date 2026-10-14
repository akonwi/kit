import SwiftUI

struct MCPStatusView: View {
    @Bindable var state: SessionStore
    let back: () -> Void
    @Environment(\.mica) private var theme

    var body: some View {
        PaletteSheet(title: "MCP servers", detail: servers.isEmpty ? nil : "\(servers.count) configured",
                     height: PaletteMetrics.height) {
            ScrollView {
                VStack(alignment: .leading, spacing: 8) {
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
                                Label(warning, icon: .warning)
                                    .font(.kit(size: 12)).foregroundStyle(theme.warning)
                                    .fixedSize(horizontal: false, vertical: true).textSelection(.enabled)
                            }
                        }.padding(.top, 8)
                    }
                }.padding(16).frame(maxWidth: .infinity, alignment: .leading)
            }
        } actions: {
            Button("Back", action: back).buttonStyle(MicaButtonStyle(compact: true))
            Spacer()
        }
        .onExitCommand { state.ui.palette = false }
        .task(id: "\(state.serverID)/\(state.selectedID)") { await state.monitorMCPStatus() }
    }

    private var servers: [MCPServerStatus] { state.mcpStatus.servers }
    private var warnings: [String] { state.mcpStatus.warnings }

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
                Label(server.lastError, icon: .warningCircle)
                    .font(.kit(size: 12)).foregroundStyle(theme.danger)
            }
        }
        .padding(12).background(theme.raised).clipShape(RoundedRectangle(cornerRadius: 4))
        .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.border))
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
            .padding(.horizontal, 6).padding(.vertical, 2).background(theme.hover).clipShape(RoundedRectangle(cornerRadius: 2))
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
