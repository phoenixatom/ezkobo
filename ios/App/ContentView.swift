import SwiftUI

struct ContentView: View {
    @StateObject private var finder = KoboFinder()
    @StateObject private var transfers = TransferQueue()
    @AppStorage("selectedKobo") private var selectedID = ""
    @State private var books: [Book] = []
    @State private var booksError: String?
    @State private var choosingFiles = false
    @State private var query = ""
    @State private var showSettings = false
    /// The Kobo that asked for a PIN, and the files to send once it's entered.
    @State private var pinPrompt: Kobo?
    @State private var pendingFiles: [URL] = []
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.openURL) private var openURL

    private var selected: Kobo? {
        finder.kobos.first { $0.id == selectedID } ?? finder.kobos.first
    }

    var body: some View {
        NavigationStack {
            List {
                if !transfers.items.isEmpty {
                    transferSection
                        .transition(.opacity)
                }
                if !finder.kobos.isEmpty {
                    koboSection
                }
                if let selected {
                    librarySection(selected)
                }
            }
            .navigationTitle("EzKobo")
            .toolbarTitleDisplayMode(.inlineLarge)
            .searchable(text: $query, placement: .navigationBarDrawer(displayMode: .automatic),
                        prompt: "Search Books")
            .overlay {
                if finder.kobos.isEmpty {
                    searchingView
                }
            }
            .refreshable { await reload() }
            .toolbar {
                if selected != nil {
                    ToolbarItem(placement: .topBarTrailing) {
                        Button("Settings", systemImage: "gearshape") { showSettings = true }
                    }
                    ToolbarItem(placement: .bottomBar) {
                        Button("Send to \(finder.info[selected!.id]?.model ?? selected!.model)") {
                            choosingFiles = true
                        }
                            .buttonStyle(.borderedProminent)
                            .disabled(transfers.isRunning)
                    }
                }
            }
            .fileImporter(isPresented: $choosingFiles, allowedContentTypes: [.data],
                          allowsMultipleSelection: true) { result in
                if case let .success(files) = result {
                    Task { await send(files) }
                }
            }
            .sheet(isPresented: $showSettings, onDismiss: { Task { await loadBooks() } }) {
                if let selected {
                    SettingsView(kobo: selected, model: finder.info[selected.id]?.model ?? selected.model,
                                 finder: finder)
                }
            }
            .sheet(item: $pinPrompt) { kobo in
                PINEntryView(title: "Enter PIN",
                             message: "\(finder.info[kobo.id]?.model ?? kobo.model) has a PIN. You can see it under EzKobo status in the Kobo’s menu.",
                             button: "Unlock") { pin in
                    await unlock(kobo, with: pin)
                }
            }
        }
        .task { finder.start() }
        .task(id: selected?.id) { await loadBooks() }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { Task { await reload() } }
        }
    }

    private var filteredBooks: [Book] {
        query.isEmpty ? books : books.filter {
            $0.displayTitle.localizedStandardContains(query) || ($0.author ?? "").localizedStandardContains(query)
        }
    }

    // MARK: Sections

    private var koboSection: some View {
        Section {
            ForEach(finder.kobos) { kobo in
                Button {
                    selectedID = kobo.id
                } label: {
                    KoboRow(kobo: kobo, info: finder.info[kobo.id],
                            reachable: !finder.unreachable.contains(kobo.id),
                            selected: kobo.id == selected?.id && finder.kobos.count > 1)
                }
                .tint(.primary)
            }
        } header: {
            Text(finder.kobos.count > 1 ? "Send To" : "Kobo")
        }
    }

    private func librarySection(_ kobo: Kobo) -> some View {
        Section {
            if let booksError {
                Text(booksError)
                    .foregroundStyle(.secondary)
            } else if books.isEmpty {
                Text("Books you send will appear here.")
                    .foregroundStyle(.secondary)
            } else if filteredBooks.isEmpty {
                Text("No books match “\(query)”.")
                    .foregroundStyle(.secondary)
            } else {
                ForEach(filteredBooks) { book in
                    BookRow(book: book)
                        .swipeActions {
                            Button("Delete", systemImage: "trash", role: .destructive) {
                                Task { await delete(book, from: kobo) }
                            }
                        }
                }
            }
        } header: {
            Text("On \(finder.info[kobo.id]?.model ?? kobo.model)")
        } footer: {
            if books.contains(where: { $0.inLibrary == false }) {
                Text(BookName.importHint)
            }
        }
    }

    private var transferSection: some View {
        Section {
            ForEach(transfers.items) { item in
                TransferRow(item: item)
            }
        } header: {
            if transfers.isRunning {
                Text("Sending")
            } else if transfers.failures.isEmpty {
                Text("Sent")
            } else {
                HStack {
                    Text("Couldn’t Send")
                    Spacer()
                    Button("Dismiss") {
                        withAnimation { transfers.clear() }
                    }
                    .font(.subheadline)
                    .textCase(nil)
                }
            }
        } footer: {
            if let imported = transfers.imported {
                Text(imported ? "Your Kobo is adding them to its library." : BookName.importHint)
            }
        }
    }

    @ViewBuilder private var searchingView: some View {
        if finder.permissionDenied {
            ContentUnavailableView {
                Label("Local Network Access Is Off", systemImage: "wifi.exclamationmark")
            } description: {
                Text("EzKobo needs Local Network access to find your Kobo.")
            } actions: {
                Button("Open Settings") {
                    openURL(URL(string: UIApplication.openSettingsURLString)!)
                }
            }
        } else {
            ContentUnavailableView {
                Label("Looking for Kobos", systemImage: "wifi")
                    .symbolEffect(.variableColor.iterative, options: .repeating)
            } description: {
                Text("Make sure your Kobo is awake and on the same Wi‑Fi network as this iPhone.")
            }
        }
    }

    // MARK: Actions

    private func reload() async {
        await finder.refreshAll()
        await loadBooks()
    }

    private func loadBooks() async {
        guard let kobo = selected, let client = await finder.client(for: kobo) else {
            books = []
            return
        }
        do {
            books = try await client.books()
            booksError = nil
        } catch let error as KoboError where error.needsPIN {
            books = []
            booksError = "This Kobo has a PIN."
            pinPrompt = kobo
        } catch {
            booksError = "Couldn’t load books from this Kobo."
        }
    }

    /// Checks a PIN against the Kobo; on success remembers it, reloads, and
    /// resumes a send that was waiting for it. Returns an error message.
    private func unlock(_ kobo: Kobo, with pin: String) async -> String? {
        guard var client = await finder.client(for: kobo) else { return "Couldn’t reach this Kobo." }
        client.pin = pin
        do {
            _ = try await client.books()
        } catch {
            return (error as? KoboError)?.needsPIN == true ? "That PIN isn’t right." : error.localizedDescription
        }
        PINStore.save(pin, for: kobo.id)
        await loadBooks()
        if !pendingFiles.isEmpty {
            let files = pendingFiles
            pendingFiles = []
            Task { await send(files) }
        }
        return nil
    }

    private func send(_ files: [URL]) async {
        guard let kobo = selected, let client = await finder.client(for: kobo) else { return }
        await transfers.send(files, to: client, securityScoped: true)
        if transfers.needsPIN {
            pendingFiles = files
            pinPrompt = kobo
            return
        }
        await loadBooks()
        await finder.refresh(kobo)

        // A successful send is confirmed briefly, then gets out of the way;
        // the books themselves are in the library list. Failures stay until
        // dismissed.
        guard transfers.failures.isEmpty else { return }
        let batch = transfers.items.map(\.id)
        try? await Task.sleep(for: .seconds(3))
        if transfers.items.map(\.id) == batch {
            withAnimation { transfers.clear() }
        }
    }

    private func delete(_ book: Book, from kobo: Kobo) async {
        guard let client = await finder.client(for: kobo) else { return }
        books.removeAll { $0.id == book.id }
        try? await client.delete(book.name)
        _ = await client.rescan()
        await loadBooks()
        await finder.refresh(kobo)
    }
}

private struct BookRow: View {
    let book: Book

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(book.displayTitle)
                .lineLimit(2)
            Text(details)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .lineLimit(1)
            if book.inLibrary == false {
                Text("Not in library yet")
                    .font(.subheadline)
                    .foregroundStyle(.orange)
            }
        }
        .padding(.vertical, 2)
    }

    private var details: String {
        var parts: [String] = []
        if let author = book.author, !author.isEmpty { parts.append(author) }
        parts.append(BookName.format(book.name))
        if let progress = book.progress, progress > 0 {
            parts.append("\(progress)% read")
        } else {
            parts.append(ByteCountFormatter.string(fromByteCount: book.size, countStyle: .file))
        }
        return parts.joined(separator: " · ")
    }
}
