import SwiftUI
import UIKit
import UniformTypeIdentifiers

/// "EzKobo" in the share sheet: pick a Kobo, and the shared files are sent to it.
final class ShareViewController: UIViewController {
    override func viewDidLoad() {
        super.viewDidLoad()
        let items = extensionContext?.inputItems as? [NSExtensionItem] ?? []
        let model = ShareModel(items: items) { [weak self] cancelled in
            if cancelled {
                self?.extensionContext?.cancelRequest(withError: CocoaError(.userCancelled))
            } else {
                self?.extensionContext?.completeRequest(returningItems: nil)
            }
        }
        let host = UIHostingController(rootView: ShareView(model: model, finder: model.finder,
                                                           transfers: model.transfers))
        addChild(host)
        host.view.frame = view.bounds
        host.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(host.view)
        host.didMove(toParent: self)
    }
}

@MainActor
final class ShareModel: ObservableObject {
    enum Phase: Equatable {
        case choosing, sending, done
        case failed(String)
    }

    @Published private(set) var phase = Phase.choosing
    @Published private(set) var files: [URL] = []
    @Published private(set) var target: Kobo?
    let finder = KoboFinder()
    let transfers = TransferQueue()

    private let items: [NSExtensionItem]
    private let finish: (_ cancelled: Bool) -> Void
    private static let lastKoboKey = "lastKobo"

    init(items: [NSExtensionItem], finish: @escaping (_ cancelled: Bool) -> Void) {
        self.items = items
        self.finish = finish
    }

    /// Kobos to offer, the one used last time first.
    var kobos: [Kobo] {
        let last = UserDefaults.standard.string(forKey: Self.lastKoboKey)
        return finder.kobos.sorted { a, _ in a.id == last }
    }

    func start() async {
        finder.start()
        files = await loadFiles()
        if files.isEmpty {
            phase = .failed("There’s nothing here EzKobo can send.")
        }
    }

    func send(to kobo: Kobo) async {
        target = kobo
        phase = .sending
        UserDefaults.standard.set(kobo.id, forKey: Self.lastKoboKey)
        guard let client = await finder.client(for: kobo) else {
            phase = .failed("Couldn’t reach \(finder.name(of: kobo)).")
            return
        }
        await transfers.send(files, to: client)
        if transfers.needsPIN {
            phase = .choosing
            pinPrompt = kobo
            return
        }
        if let failure = transfers.failures.first {
            phase = .failed(failure)
        } else {
            phase = .done
            // Leave time to read the import hint when the Kobo can't import by itself.
            try? await Task.sleep(for: .seconds(transfers.imported == false ? 4 : 1))
            finish(false)
        }
    }

    /// Set when a Kobo asked for a PIN; the view shows a PIN sheet.
    @Published var pinPrompt: Kobo?

    /// Checks the PIN; on success remembers it and sends. Returns an error message.
    func unlock(_ kobo: Kobo, with pin: String) async -> String? {
        guard var client = await finder.client(for: kobo) else { return "Couldn’t reach \(finder.name(of: kobo))." }
        client.pin = pin
        do {
            _ = try await client.books()
        } catch {
            return (error as? KoboError)?.needsPIN == true ? "That PIN isn’t right." : error.localizedDescription
        }
        PINStore.save(pin, for: kobo.id)
        Task { await send(to: kobo) }
        return nil
    }

    func cancel() { finish(true) }
    func close() { finish(false) }

    /// Copies the shared files somewhere we can read them for the whole upload.
    private func loadFiles() async -> [URL] {
        let dir = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        var urls: [URL] = []
        for provider in items.flatMap({ $0.attachments ?? [] }) {
            if let url = try? await Self.copy(provider, into: dir) {
                urls.append(url)
            }
        }
        return urls
    }

    private static func copy(_ provider: NSItemProvider, into dir: URL) async throws -> URL {
        let type = provider.registeredTypeIdentifiers.first { UTType($0)?.conforms(to: .data) == true }
            ?? UTType.data.identifier
        return try await withCheckedThrowingContinuation { continuation in
            _ = provider.loadFileRepresentation(forTypeIdentifier: type) { url, error in
                guard let url else {
                    continuation.resume(throwing: error ?? CocoaError(.fileReadUnknown))
                    return
                }
                let destination = dir.appending(path: url.lastPathComponent)
                do {
                    try FileManager.default.copyItem(at: url, to: destination)
                    continuation.resume(returning: destination)
                } catch {
                    continuation.resume(throwing: error)
                }
            }
        }
    }
}

struct ShareView: View {
    @ObservedObject var model: ShareModel
    @ObservedObject var finder: KoboFinder
    @ObservedObject var transfers: TransferQueue

    var body: some View {
        NavigationStack {
            List {
                switch model.phase {
                case .choosing:
                    chooseSection
                case .sending, .done:
                    sendingSection
                case .failed(let message):
                    Section {
                        Label(message, systemImage: "exclamationmark.triangle")
                    }
                    if !transfers.items.isEmpty {
                        sendingSection
                    }
                }
            }
            .navigationTitle("Send to Kobo")
            .navigationBarTitleDisplayMode(.inline)
            .overlay {
                if model.phase == .choosing && finder.kobos.isEmpty {
                    ContentUnavailableView {
                        Label("Looking for Kobos", systemImage: "wifi")
                            .symbolEffect(.variableColor.iterative, options: .repeating)
                    } description: {
                        Text("Make sure your Kobo is awake and on the same Wi‑Fi network.")
                    }
                }
            }
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    if model.phase == .choosing || model.phase == .sending {
                        Button("Cancel", role: .cancel) { model.cancel() }
                    }
                }
                ToolbarItem(placement: .confirmationAction) {
                    if case .failed = model.phase {
                        Button("Done") { model.close() }
                    }
                }
            }
        }
        .task { await model.start() }
        .sheet(item: $model.pinPrompt) { kobo in
            PINEntryView(title: "Enter PIN",
                         message: "\(finder.name(of: kobo)) has a PIN. You can see it under EzKobo status in the Kobo’s menu.",
                         button: "Send") { pin in
                await model.unlock(kobo, with: pin)
            }
        }
    }

    private var chooseSection: some View {
        Section {
            ForEach(model.kobos) { kobo in
                Button {
                    Task { await model.send(to: kobo) }
                } label: {
                    KoboRow(kobo: kobo, info: finder.info[kobo.id],
                            reachable: !finder.unreachable.contains(kobo.id))
                }
                .tint(.primary)
                .disabled(model.files.isEmpty)
            }
        } header: {
            if !finder.kobos.isEmpty {
                Text(model.files.count == 1 ? model.files[0].lastPathComponent : "\(model.files.count) files")
                    .textCase(nil)
                    .lineLimit(1)
                    .truncationMode(.middle)
            }
        }
    }

    private var sendingSection: some View {
        Section {
            ForEach(transfers.items) { item in
                TransferRow(item: item)
            }
        } header: {
            if let target = model.target {
                Text(model.phase == .done ? "Sent to \(finder.name(of: target))" : "Sending to \(finder.name(of: target))")
            }
        } footer: {
            if transfers.imported == false {
                Text(BookName.importHint)
            }
        }
    }
}
