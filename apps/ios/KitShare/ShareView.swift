import SwiftUI

/// The share sheet: pick a session, then add a note and add what was shared
/// to the session's draft.
struct ShareView: View {
    let model: ShareModel
    @State private var search = ""

    var body: some View {
        NavigationStack {
            content
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Cancel", systemImage: "xmark", action: model.cancel)
                    }
                    ToolbarItem(placement: .principal) {
                        VStack(spacing: 0) {
                            Text("Share to Kit").font(.headline)
                            if let subtitle {
                                Text(subtitle).font(.caption).foregroundStyle(Theme.muted).lineLimit(1)
                            }
                        }
                    }
                }
                .navigationDestination(for: ShareDestination.self) { destination in
                    ShareCompose(model: model, destination: destination)
                }
        }
    }

    @ViewBuilder private var content: some View {
        switch model.shared {
        case .nothing:
            ContentUnavailableView("Nothing to share", systemImage: "square.and.arrow.up",
                                   description: Text("Kit takes web links."))
        case .reading, .link:
            if !model.paired {
                ContentUnavailableView("No servers", systemImage: "server.rack",
                                       description: Text("Pair a server in Kit first."))
            } else {
                sessions
            }
        }
    }

    private var sessions: some View {
        let shown = model.destinations(matching: search)
        return List {
            Section {
                ForEach(shown) { destination in
                    NavigationLink(value: destination) {
                        DestinationLabel(destination: destination)
                    }
                }
            } footer: {
                if !model.problems.isEmpty {
                    VStack(alignment: .leading, spacing: 4) {
                        ForEach(model.problems, id: \.server) { problem in
                            Text("\(problem.server): \(problem.reason)")
                        }
                    }
                }
            }
        }
        .overlay {
            if shown.isEmpty {
                if model.loading {
                    ProgressView()
                } else if !search.isEmpty {
                    ContentUnavailableView.search(text: search)
                } else if model.problems.isEmpty {
                    ContentUnavailableView("No sessions", systemImage: "apple.terminal",
                                           description: Text("Start a session in Kit first."))
                }
            }
        }
        .searchable(text: $search, placement: .navigationBarDrawer(displayMode: .always), prompt: "Sessions")
    }

    private var subtitle: String? {
        guard case .link(let link) = model.shared else { return nil }
        return link.title ?? link.url.host()
    }
}

/// A session's title and where it runs.
private struct DestinationLabel: View {
    let destination: ShareDestination

    var body: some View {
        VStack(alignment: .leading, spacing: 1) {
            Text(destination.title)
                .font(.system(size: 17))
                .foregroundStyle(Theme.text)
                .lineLimit(1)
            Text("\(destination.server) · \(destination.folder)")
                .font(.system(size: 12, design: .monospaced))
                .foregroundStyle(Theme.muted)
                .lineLimit(1)
                .truncationMode(.middle)
        }
        .padding(.vertical, 2)
    }
}

/// What was shared, the session it goes to, a note, and the button that
/// adds them to the session's draft.
private struct ShareCompose: View {
    let model: ShareModel
    let destination: ShareDestination
    @State private var note = ""

    var body: some View {
        List {
            if case .link(let link) = model.shared {
                Section {
                    LinkCard(link: link)
                }
            }
            Section("Session") {
                DestinationLabel(destination: destination)
            }
            Section("Note") {
                TextField("Optional", text: $note, axis: .vertical)
                    .lineLimit(3...8)
            }
        }
        .safeAreaInset(edge: .bottom) {
            Button {
                model.addToDraft(destination, note: note)
            } label: {
                Text("Add to draft")
                    .font(.system(size: 17, weight: .semibold))
                    .frame(maxWidth: .infinity, minHeight: 34)
            }
            .buttonStyle(.borderedProminent)
            .buttonBorderShape(.capsule)
            .padding(.horizontal, 16)
            .padding(.bottom, 8)
        }
        .navigationTitle("Share to Kit")
        .alert("Couldn’t add it", isPresented: Binding { model.failure != nil } set: { if !$0 { model.failure = nil } }) {
            Button("OK") {}
        } message: {
            Text(model.failure ?? "")
        }
    }
}

/// A shared link: its page's title and its address.
private struct LinkCard: View {
    let link: ShareModel.Link

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: "link")
                .font(.system(size: 17))
                .foregroundStyle(Theme.muted)
                .frame(width: 40, height: 40)
                .background(Theme.hover, in: RoundedRectangle(cornerRadius: 8))
            VStack(alignment: .leading, spacing: 2) {
                if let title = link.title {
                    Text(title)
                        .font(.system(size: 15, weight: .semibold))
                        .foregroundStyle(Theme.text)
                        .lineLimit(2)
                }
                Text(link.url.absoluteString)
                    .font(.system(size: 12, design: .monospaced))
                    .foregroundStyle(Theme.muted)
                    .lineLimit(1)
                    .truncationMode(.middle)
            }
        }
    }
}
