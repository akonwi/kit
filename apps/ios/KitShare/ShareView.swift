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
                            .disabled(model.busy)
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
                                   description: Text("Kit takes web links, images, and files."))
        case .unreadable(let reason):
            ContentUnavailableView("Couldn’t read it all", systemImage: "exclamationmark.triangle",
                                   description: Text(reason))
        case .reading, .link, .files:
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
        switch model.shared {
        case .link(let link): link.title ?? link.url.host()
        case .files(let files): ShareSummary.title(files)
        case .reading, .nothing, .unreadable: nil
        }
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

/// What was shared, the session it goes to, a note, and the buttons that
/// send it or add it to the session's draft.
private struct ShareCompose: View {
    let model: ShareModel
    let destination: ShareDestination
    @State private var note = ""

    var body: some View {
        List {
            Section {
                switch model.shared {
                case .link(let link): LinkCard(link: link)
                case .files(let files): FilesCard(files: files, previews: model.previews)
                case .reading, .nothing, .unreadable: EmptyView()
                }
            }
            Section("Session") {
                DestinationLabel(destination: destination)
            }
            Section("Note") {
                TextField("Optional", text: $note, axis: .vertical)
                    .lineLimit(3...8)
                    .disabled(model.busy || model.progress == .uncertain)
            }
        }
        .safeAreaInset(edge: .bottom) { actions }
        .navigationTitle("Share to Kit")
        .navigationBarBackButtonHidden(model.busy)
        .interactiveDismissDisabled(model.busy)
        .alert("Couldn’t share it", isPresented: Binding { model.failure != nil } set: { if !$0 { model.failure = nil } }) {
            Button("OK") {}
        } message: {
            Text(model.failure ?? "")
        }
        .alert("It may not have been sent", isPresented: .constant(model.progress == .uncertain)) {
            Button("Add to Draft Instead", action: model.keepUnsent)
            Button("Done", role: .cancel) { model.done() }
        } message: {
            Text("The connection dropped before \(destination.server) answered. Check the session in Kit before sending it again.")
        }
    }

    private var actions: some View {
        VStack(spacing: 8) {
            if model.progress == .uploading {
                Label("Uploading…", systemImage: "arrow.up.circle")
                    .font(.system(size: 13))
                    .foregroundStyle(Theme.muted)
            }
            HStack(spacing: 10) {
                Button {
                    model.send(destination, note: note)
                } label: {
                    Group {
                        if model.progress == .sending { ProgressView() } else { Text("Send now") }
                    }
                    .font(.system(size: 17, weight: .semibold))
                    .frame(maxWidth: .infinity, minHeight: 34)
                }
                .buttonStyle(.bordered)
                Button {
                    model.addToDraft(destination, note: note)
                } label: {
                    Text("Add to draft")
                        .font(.system(size: 17, weight: .semibold))
                        .frame(maxWidth: .infinity, minHeight: 34)
                }
                .buttonStyle(.borderedProminent)
                .layoutPriority(1)
            }
            .buttonBorderShape(.capsule)
            .disabled(model.busy)
        }
        .padding(.horizontal, 16)
        .padding(.bottom, 8)
    }
}

/// Shared images and files: up to four thumbnails, how many, and their names.
private struct FilesCard: View {
    let files: [ShareModel.File]
    let previews: [UUID: UIImage]

    var body: some View {
        HStack(spacing: 12) {
            HStack(spacing: 4) {
                ForEach(files.prefix(files.count > 4 ? 3 : 4)) { file in
                    thumbnail(file)
                }
                if files.count > 4 {
                    Text("+\(files.count - 3)")
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(Theme.muted)
                        .frame(width: 40, height: 40)
                        .background(Theme.hover, in: RoundedRectangle(cornerRadius: 8))
                }
            }
            VStack(alignment: .leading, spacing: 2) {
                Text(ShareSummary.title(files))
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(Theme.text)
                Text(files.map(\.name).joined(separator: ", "))
                    .font(.system(size: 12, design: .monospaced))
                    .foregroundStyle(Theme.muted)
                    .lineLimit(2)
                    .truncationMode(.middle)
            }
        }
    }

    @ViewBuilder private func thumbnail(_ file: ShareModel.File) -> some View {
        if let preview = previews[file.id] {
            Image(uiImage: preview)
                .resizable()
                .scaledToFill()
                .frame(width: 40, height: 40)
                .clipShape(RoundedRectangle(cornerRadius: 8))
        } else {
            Image(systemName: file.isImage ? "photo" : "doc")
                .font(.system(size: 17))
                .foregroundStyle(Theme.muted)
                .frame(width: 40, height: 40)
                .background(Theme.hover, in: RoundedRectangle(cornerRadius: 8))
        }
    }
}

/// How shared files are named together.
enum ShareSummary {
    static func title(_ files: [ShareModel.File]) -> String {
        if files.count == 1 { return files[0].name }
        if files.allSatisfy(\.isImage) { return "\(files.count) images" }
        return "\(files.count) files"
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
