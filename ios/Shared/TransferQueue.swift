import Foundation

/// Uploads files to a Kobo one at a time and tracks their progress.
@MainActor
final class TransferQueue: ObservableObject {
    struct Item: Identifiable {
        enum State: Equatable {
            case waiting, sending, sent, skipped
            case failed(String)
        }

        let id = UUID()
        let name: String
        var progress: Double = 0
        var state: State = .waiting
    }

    @Published private(set) var items: [Item] = []
    @Published private(set) var isRunning = false
    /// After sending: whether the Kobo started importing the new books.
    @Published private(set) var imported: Bool?

    var failures: [String] {
        items.compactMap { if case let .failed(message) = $0.state { message } else { nil } }
    }

    func send(_ files: [URL], to client: KoboClient, securityScoped: Bool = false) async {
        items = files.map { Item(name: $0.lastPathComponent) }
        imported = nil
        isRunning = true
        defer { isRunning = false }

        var sent = 0
        for (index, file) in files.enumerated() {
            items[index].state = .sending
            let scoped = securityScoped && file.startAccessingSecurityScopedResource()
            do {
                let skipped = try await client.upload(file, as: file.lastPathComponent) { fraction in
                    Task { @MainActor [weak self] in
                        guard let self, self.items.indices.contains(index),
                              self.items[index].state == .sending else { return }
                        self.items[index].progress = fraction
                    }
                }
                items[index].progress = 1
                items[index].state = skipped ? .skipped : .sent
                if !skipped { sent += 1 }
            } catch {
                items[index].state = .failed(error.localizedDescription)
            }
            if scoped { file.stopAccessingSecurityScopedResource() }
        }
        if sent > 0 {
            imported = await client.rescan()
        }
    }

    func clear() {
        guard !isRunning else { return }
        items = []
        imported = nil
    }
}
