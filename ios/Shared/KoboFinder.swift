import Foundation
import Network

/// Browses the local network for Kobos advertising `_ezkobo._tcp` and keeps
/// their details (battery, storage…) up to date.
@MainActor
final class KoboFinder: ObservableObject {
    @Published private(set) var kobos: [Kobo] = []
    @Published private(set) var info: [Kobo.ID: KoboInfo] = [:]
    /// Kobos that are advertised but didn't answer, e.g. asleep.
    @Published private(set) var unreachable: Set<Kobo.ID> = []
    @Published private(set) var permissionDenied = false

    private var browser: NWBrowser?
    private var resolved: [Kobo.ID: URL] = [:]

    func start() {
        guard browser == nil else { return }
        let browser = NWBrowser(for: .bonjourWithTXTRecord(type: "_ezkobo._tcp", domain: nil), using: .tcp)
        browser.stateUpdateHandler = { [weak self] state in
            Task { @MainActor in self?.handle(state) }
        }
        browser.browseResultsChangedHandler = { [weak self] results, _ in
            Task { @MainActor in self?.update(results) }
        }
        browser.start(queue: .main)
        self.browser = browser
    }

    /// The name set in Settings, or the model.
    func name(of kobo: Kobo) -> String {
        if let n = info[kobo.id]?.displayName, !n.isEmpty { return n }
        return info[kobo.id]?.model ?? kobo.model
    }

    func client(for kobo: Kobo) async -> KoboClient? {
        await url(for: kobo).map { KoboClient(base: $0, pin: PINStore.pin(for: kobo.id)) }
    }

    func refresh(_ kobo: Kobo) async {
        guard let client = await client(for: kobo) else {
            unreachable.insert(kobo.id)
            return
        }
        do {
            info[kobo.id] = try await client.info()
            unreachable.remove(kobo.id)
        } catch {
            resolved[kobo.id] = nil
            unreachable.insert(kobo.id)
        }
    }

    func refreshAll() async {
        await withTaskGroup(of: Void.self) { group in
            for kobo in kobos {
                group.addTask { await self.refresh(kobo) }
            }
        }
    }

    private func handle(_ state: NWBrowser.State) {
        switch state {
        case .ready:
            permissionDenied = false
        case .waiting(let error):
            // kDNSServiceErr_PolicyDenied: Local Network access was declined.
            if case .dns(let code) = error, code == -65570 {
                permissionDenied = true
            }
        case .failed:
            browser?.cancel()
            browser = nil
            Task {
                try? await Task.sleep(for: .seconds(2))
                start()
            }
        default:
            break
        }
    }

    private func update(_ results: Set<NWBrowser.Result>) {
        let previous = Set(kobos.map(\.id))
        kobos = results.compactMap { result -> Kobo? in
            guard case let .service(name, _, _, _) = result.endpoint else { return nil }
            var model = name
            var address: URL?
            if case let .bonjour(txt) = result.metadata {
                if let m = txt["model"], !m.isEmpty { model = m }
                if let ip = txt["ip"], let port = txt["port"] {
                    address = URL(string: "http://\(ip):\(port)")
                }
            }
            return Kobo(id: name, model: model, endpoint: result.endpoint, address: address)
        }
        .sorted { $0.id.localizedStandardCompare($1.id) == .orderedAscending }

        for kobo in kobos where !previous.contains(kobo.id) || info[kobo.id] == nil {
            Task { await refresh(kobo) }
        }
    }

    private func url(for kobo: Kobo) async -> URL? {
        if let address = kobo.address { return address }
        if let url = resolved[kobo.id] { return url }
        let url = await Self.resolve(kobo.endpoint)
        resolved[kobo.id] = url
        return url
    }

    /// Resolves a Bonjour service to an IPv4 URL by briefly connecting to it.
    private static func resolve(_ endpoint: NWEndpoint) async -> URL? {
        await withCheckedContinuation { continuation in
            let connection = NWConnection(to: endpoint, using: .tcp)
            let once = Once()
            let finish: @Sendable (URL?) -> Void = { url in
                once.run {
                    continuation.resume(returning: url)
                    connection.cancel()
                }
            }
            connection.stateUpdateHandler = { state in
                switch state {
                case .ready:
                    if case let .hostPort(host, port)? = connection.currentPath?.remoteEndpoint,
                       case let .ipv4(address) = host {
                        finish(URL(string: "http://\(address):\(port.rawValue)"))
                    } else {
                        finish(nil)
                    }
                case .failed, .cancelled:
                    finish(nil)
                default:
                    break
                }
            }
            connection.start(queue: .global())
            DispatchQueue.global().asyncAfter(deadline: .now() + 5) { finish(nil) }
        }
    }
}

private final class Once: @unchecked Sendable {
    private let lock = NSLock()
    private var done = false

    func run(_ body: () -> Void) {
        lock.lock()
        defer { lock.unlock() }
        guard !done else { return }
        done = true
        body()
    }
}
