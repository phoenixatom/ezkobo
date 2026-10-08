import SwiftUI

/// Home: the selected Kobo, what's being read, stats, and the way into the
/// library. Switch Kobos from the title menu.
struct ContentView: View {
    @StateObject private var finder = KoboFinder()
    @StateObject private var transfers = TransferQueue()
    @AppStorage("selectedKobo") private var selectedID = ""
    @State private var books: [Book] = []
    @State private var client: KoboClient?
    @State private var summary: ReadingSummary?
    @State private var booksError: String?
    @State private var choosingFiles = false
    @State private var showSettings = false
    /// The Kobo that asked for a PIN, and the files to send once it's entered.
    @State private var pinPrompt: Kobo?
    @State private var pendingFiles: [URL] = []
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.openURL) private var openURL

    private var selected: Kobo? {
        finder.kobos.first { $0.id == selectedID } ?? finder.kobos.first
    }

    private func model(_ kobo: Kobo) -> String { finder.info[kobo.id]?.model ?? kobo.model }

    var body: some View {
        NavigationStack {
            ScrollView {
                if let selected {
                    home(selected)
                } else {
                    searchingView
                        .padding(.top, 90)
                }
            }
            .background(Color(.systemGroupedBackground))
            .navigationTitle(selected.map(model) ?? "EzKobo")
            .navigationSubtitle(subtitle)
            .toolbarTitleDisplayMode(.inlineLarge)
            .toolbarTitleMenu {
                if finder.kobos.count > 1 {
                    ForEach(finder.kobos) { kobo in
                        Button {
                            selectedID = kobo.id
                        } label: {
                            Label(menuLabel(kobo), systemImage: kobo.id == selected?.id ? "checkmark" : "")
                        }
                    }
                }
            }
            .refreshable { await reload() }
            .toolbar {
                if selected != nil {
                    ToolbarItem(placement: .topBarTrailing) {
                        Button("Settings", systemImage: "gearshape") { showSettings = true }
                    }
                }
            }
            .scrollEdgeEffectStyle(.soft, for: .bottom)
            .safeAreaBar(edge: .bottom) {
                if selected != nil {
                    Button {
                        choosingFiles = true
                    } label: {
                        Label("Send Books", systemImage: "plus")
                            .font(.headline)
                            .padding(.horizontal, 10)
                            .padding(.vertical, 6)
                    }
                    .buttonStyle(.glassProminent)
                    .controlSize(.large)
                    .disabled(transfers.isRunning)
                    .padding(.bottom, 8)
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
                    SettingsView(kobo: selected, model: model(selected), finder: finder)
                }
            }
            .sheet(item: $pinPrompt) { kobo in
                PINEntryView(title: "Enter PIN",
                             message: "\(model(kobo)) has a PIN. You can see it under EzKobo status in the Kobo’s menu.",
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

    // MARK: Home

    private func home(_ kobo: Kobo) -> some View {
        VStack(spacing: 14) {
            if finder.kobos.count > 1 {
                koboSwitcher(selected: kobo)
                    .padding(.bottom, 4)
            }
            if !transfers.items.isEmpty {
                TransferCard(queue: transfers)
                    .transition(.move(edge: .top).combined(with: .opacity))
                    .padding(.bottom, 10)
            }

            if finder.unreachable.contains(kobo.id) {
                MascotMessage(title: "\(model(kobo)) Is Asleep",
                              message: "Wake it up and turn on Wi‑Fi to send books and see your reading.",
                              size: 150)
                    .padding(.vertical, 30)
            } else if let booksError {
                VStack(spacing: 18) {
                    MascotMessage(title: "Couldn’t Load Books", message: booksError, size: 150)
                    Button("Try Again") { Task { await reload() } }
                        .buttonStyle(.glass)
                }
                .padding(.vertical, 30)
            } else {
                readingCards(kobo)
                libraryCard(kobo)
            }
        }
        .padding(.horizontal, 20)
        .padding(.top, 8)
        .padding(.bottom, 24)
        .animation(.default, value: transfers.items.isEmpty)
    }

    /// One tap to switch Kobos: a capsule per Kobo, the selected one filled.
    private func koboSwitcher(selected: Kobo) -> some View {
        ScrollView(.horizontal) {
            HStack(spacing: 10) {
                ForEach(finder.kobos) { kobo in
                    let isSelected = kobo.id == selected.id
                    Button {
                        withAnimation(.snappy) { selectedID = kobo.id }
                    } label: {
                        HStack(spacing: 8) {
                            DeviceGlyph(model: model(kobo))
                                .scaleEffect(0.55)
                                .frame(width: 20, height: 22)
                            Text(switcherName(kobo))
                                .font(.subheadline.weight(.semibold))
                        }
                        .padding(.horizontal, 4)
                    }
                    .buttonStyle(isSelected ? AnyPrimitiveButtonStyle(.glassProminent) : AnyPrimitiveButtonStyle(.glass))
                    .accessibilityAddTraits(isSelected ? .isSelected : [])
                }
            }
            .padding(.horizontal, 20)
        }
        .scrollIndicators(.hidden)
        .padding(.horizontal, -20)
    }

    /// The model, plus the end of the serial when two Kobos share a model.
    private func switcherName(_ kobo: Kobo) -> String {
        let name = model(kobo)
        let sameModel = finder.kobos.filter { model($0) == name }.count > 1
        if sameModel, let serial = finder.info[kobo.id]?.serial, !serial.isEmpty {
            return "\(name) \(serial)"
        }
        return name
    }

    @ViewBuilder private func readingCards(_ kobo: Kobo) -> some View {
        if let summary {
            NavigationLink {
                StatsView(client: client, summary: summary, model: model(kobo))
            } label: {
                StatsStrip(stats: summary.stats)
                    .foregroundStyle(.primary)
                    .card()
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Reading Stats")
            .padding(.bottom, 12)

            if let current = summary.reading.first {
                HomeSectionTitle("Continue Reading")
                NavigationLink {
                    BookDetailView(client: client, bookID: current.id, fallbackTitle: current.title)
                } label: {
                    ContinueReadingCard(client: client, book: current)
                }
                .buttonStyle(.plain)

                if summary.reading.count > 1 {
                    HomeSectionTitle("Also Reading")
                        .padding(.top, 12)
                    ReadingShelf(client: client, books: Array(summary.reading.dropFirst()))
                }
            } else {
                MascotMessage(image: "MascotReading", title: "Nothing in Progress",
                              message: "Send a book and it’ll be waiting on your Kobo.", size: 140)
                    .card()
            }

        }
    }

    private func libraryCard(_ kobo: Kobo) -> some View {
        NavigationLink {
            LibraryView(client: client, model: model(kobo), books: books,
                        delete: { await delete($0, from: kobo) }, reload: { await loadBooks() })
        } label: {
            LibraryCard(client: client, covers: libraryCovers, count: books.count,
                        notImported: books.filter { $0.inLibrary == false }.count)
        }
        .buttonStyle(.plain)
        .padding(.top, summary == nil ? 0 : 12)
    }

    /// Covers for the library card: recently read first.
    private var libraryCovers: [ReadingBook] {
        guard let summary else { return [] }
        var seen = Set<String>()
        return (summary.reading + summary.finished + summary.byTime)
            .filter { $0.cover && seen.insert($0.id).inserted }
    }

    private var subtitle: String {
        guard let kobo = selected else { return "" }
        if finder.unreachable.contains(kobo.id) { return "Not responding" }
        guard let info = finder.info[kobo.id] else { return "Connecting…" }
        var parts: [String] = []
        if let battery = info.battery { parts.append("\(battery.level)%\(battery.charging ? " charging" : "")") }
        parts.append("\(ByteCountFormatter.string(fromByteCount: info.free, countStyle: .file)) free")
        if info.locked == true { parts.append("PIN") }
        return parts.joined(separator: " · ")
    }

    private func menuLabel(_ kobo: Kobo) -> String {
        guard let info = finder.info[kobo.id] else { return model(kobo) }
        let serial = info.serial.isEmpty ? "" : " \(info.serial)"
        return "\(info.model)\(serial)"
    }

    @ViewBuilder private var searchingView: some View {
        if finder.permissionDenied {
            VStack(spacing: 20) {
                MascotMessage(title: "Local Network Access Is Off",
                              message: "EzKobo needs Local Network access to find your Kobo.")
                Button("Open Settings") {
                    openURL(URL(string: UIApplication.openSettingsURLString)!)
                }
                .buttonStyle(.glass)
            }
        } else {
            MascotMessage(image: "MascotReading", title: "Looking for Your Kobo",
                          message: "Make sure it’s awake and on the same Wi‑Fi as this iPhone.",
                          size: 200, bounce: true)
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
            summary = nil
            return
        }
        self.client = client
        do {
            books = try await client.books()
            booksError = nil
            summary = try? await client.reading()
        } catch let error as KoboError where error.needsPIN {
            books = []
            summary = nil
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

        // A successful send is confirmed briefly, then gets out of the way.
        // Failures stay until dismissed.
        guard transfers.failures.isEmpty else { return }
        let batch = transfers.items.map(\.id)
        try? await Task.sleep(for: .seconds(3))
        if transfers.items.map(\.id) == batch {
            withAnimation { transfers.clear() }
        }
    }

    private func delete(_ book: Book, from kobo: Kobo) async {
        guard let client = await finder.client(for: kobo) else { return }
        books.removeAll { $0.name == book.name }
        try? await client.delete(book.name)
        _ = await client.rescan()
        await loadBooks()
        await finder.refresh(kobo)
    }
}

/// Lets a button switch between two primitive styles (glass and glassProminent).
struct AnyPrimitiveButtonStyle: PrimitiveButtonStyle {
    private let make: (Configuration) -> AnyView

    init<S: PrimitiveButtonStyle>(_ style: S) {
        make = { AnyView(style.makeBody(configuration: $0)) }
    }

    func makeBody(configuration: Configuration) -> some View { make(configuration) }
}
