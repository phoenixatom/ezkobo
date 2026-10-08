import Foundation

/// Uploads files to a Kobo one at a time and tracks their progress.
@MainActor
final class TransferQueue: ObservableObject {
    struct Item: Identifiable {
        enum State: Equatable {
            case waiting, sending, processing, sent, skipped
            case failed(String)
        }

        let id = UUID()
        var name: String
        var progress: Double = 0
        var state: State = .waiting
        /// What the Kobo did, e.g. "Converted to KEPUB · Details fixed".
        var note: String?
    }

    @Published private(set) var items: [Item] = []
    @Published private(set) var isRunning = false
    /// After sending: whether the Kobo started importing the new books.
    @Published private(set) var imported: Bool?

    var failures: [String] {
        items.compactMap { if case let .failed(message) = $0.state { message } else { nil } }
    }

    /// Set when the Kobo asked for a PIN; the caller should ask and retry.
    @Published private(set) var needsPIN = false

    func send(_ files: [URL], to client: KoboClient, securityScoped: Bool = false) async {
        items = files.map { Item(name: $0.lastPathComponent) }
        imported = nil
        needsPIN = false
        isRunning = true
        defer { isRunning = false }

        var sent = 0
        for (index, file) in files.enumerated() {
            items[index].state = .sending
            let scoped = securityScoped && file.startAccessingSecurityScopedResource()
            do {
                let result = try await client.upload(file, as: file.lastPathComponent) { fraction in
                    Task { @MainActor [weak self] in
                        guard let self, self.items.indices.contains(index),
                              self.items[index].state == .sending else { return }
                        self.items[index].progress = fraction
                        // Everything's sent; the Kobo is converting or looking up details.
                        if fraction >= 1 { self.items[index].state = .processing }
                    }
                }
                items[index].progress = 1
                items[index].name = result.name
                items[index].state = result.skipped == true ? .skipped : .sent
                items[index].note = Self.note(for: result)
                if result.skipped != true { sent += 1 }
            } catch let error as KoboError where error.needsPIN {
                items[index].state = .failed(error.localizedDescription)
                needsPIN = true
                if scoped { file.stopAccessingSecurityScopedResource() }
                break
            } catch {
                items[index].state = .failed(error.localizedDescription)
            }
            if scoped { file.stopAccessingSecurityScopedResource() }
        }
        if sent > 0 {
            imported = await client.rescan()
        }
    }

    private static func note(for result: UploadResult) -> String? {
        var parts: [String] = []
        if result.converted == true { parts.append("Converted to KEPUB") }
        switch result.metadata {
        case "openlibrary": parts.append("Details from Open Library")
        case "google": parts.append("Details from Google Books")
        case "cleaned": parts.append("Name cleaned up")
        default: break
        }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    func clear() {
        guard !isRunning else { return }
        items = []
        imported = nil
    }
}
