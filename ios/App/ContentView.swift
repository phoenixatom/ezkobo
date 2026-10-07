import SwiftUI

struct ContentView: View {
    @StateObject private var finder = KoboFinder()
    @StateObject private var transfers = TransferQueue()
    @AppStorage("selectedKobo") private var selectedID = ""
    @State private var books: [Book] = []
    @State private var booksError: String?
    @State private var choosingFiles = false
    @State private var query = ""
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
        }
        .task { finder.start() }
        .task(id: selected?.id) { await loadBooks() }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { Task { await reload() } }
        }
    }

    private var filteredBooks: [Book] {
        query.isEmpty ? books : books.filter { $0.name.localizedStandardContains(query) }
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
        } catch {
            booksError = "Couldn’t load books from this Kobo."
        }
    }

    private func send(_ files: [URL]) async {
        guard let kobo = selected, let client = await finder.client(for: kobo) else { return }
        await transfers.send(files, to: client, securityScoped: true)
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
            Text(title)
                .lineLimit(2)
            Text("\(format) · \(ByteCountFormatter.string(fromByteCount: book.size, countStyle: .file)) · \(book.date.formatted(.relative(presentation: .named)))")
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
        .padding(.vertical, 2)
    }

    private var title: String { BookName.title(book.name) }
    private var format: String { BookName.format(book.name) }
}
