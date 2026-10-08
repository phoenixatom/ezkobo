import Foundation
import Network

/// A Kobo running the EzKobo agent, found on the local network via Bonjour.
struct Kobo: Identifiable, Hashable {
    /// Bonjour service name, e.g. "Libra Colour 1A2B". Unique per device.
    let id: String
    let model: String
    let endpoint: NWEndpoint
    /// Address advertised in the TXT record, if any.
    let address: URL?
}

struct KoboInfo: Decodable, Hashable {
    struct Battery: Decodable, Hashable {
        let level: Int
        let charging: Bool
    }

    let name: String
    let model: String
    let serial: String
    let firmware: String
    let battery: Battery?
    let free: Int64
    let total: Int64
    let books: Int
    /// Whether this Kobo requires a PIN (nil from older agents).
    let locked: Bool?
    /// What to call it: the name set in Settings, or the model.
    let displayName: String?
}

/// Per-Kobo settings, stored on the Kobo itself.
struct KoboSettings: Codable, Hashable {
    /// Custom name; empty means automatic.
    var name: String?
    /// Library IDs hidden from the app's reading lists.
    var hidden: [String]?
    var kepub: Bool
    var metadata: Bool
    var cleanNames: Bool
    /// Where to look up book details, in order.
    var providers: [DetailsProvider]
    /// Whether keys are stored on the Kobo; the keys themselves are never sent back.
    var googleApiKeySet: Bool
    var hardcoverTokenSet: Bool

    /// Only the editable fields are sent back. The hidden list changes through
    /// `setHidden`, so a stale copy here can never undo a hide or unhide.
    private enum EncodedKeys: String, CodingKey { case name, kepub, metadata, cleanNames, providers }

    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: EncodedKeys.self)
        try c.encode(name ?? "", forKey: .name)
        try c.encode(kepub, forKey: .kepub)
        try c.encode(metadata, forKey: .metadata)
        try c.encode(cleanNames, forKey: .cleanNames)
        try c.encode(providers, forKey: .providers)
    }
}

struct DetailsProvider: Codable, Hashable, Identifiable {
    let id: String
    var enabled: Bool

    var name: String {
        switch id {
        case "apple": "Apple Books"
        case "openlibrary": "Open Library"
        case "google": "Google Books"
        case "hardcover": "Hardcover"
        default: id
        }
    }
}

/// What the Kobo did with an uploaded book.
struct UploadResult: Decodable {
    let name: String
    let skipped: Bool?
    let converted: Bool?
    let metadata: String?
}

struct Book: Decodable, Hashable {
    /// Path relative to the Kobo's storage, e.g. "Books/Dune.epub".
    let name: String
    let size: Int64
    let mtime: TimeInterval
    /// From the Kobo's library database, when available.
    let title: String?
    let author: String?
    /// False when the file is on the Kobo but not in its library yet; nil if unknown.
    let inLibrary: Bool?
    let progress: Int?
    /// The Kobo's library ID, for details and cover.
    let id: String?

    var listID: String { name }
    var date: Date { Date(timeIntervalSince1970: mtime) }
    var displayTitle: String { title.flatMap { $0.isEmpty ? nil : $0 } ?? BookName.title(name) }
}

/// How a book's file name is shown: "Dune.kepub.epub" → "Dune", "KEPUB".
enum BookName {
    /// `name` may be a path like "Books/Dune.epub"; only the file name is shown.
    static func title(_ name: String) -> String {
        let file = (name as NSString).lastPathComponent
        if file.lowercased().hasSuffix(".kepub.epub") { return String(file.dropLast(11)) }
        return (file as NSString).deletingPathExtension
    }

    /// Shown after sending when the Kobo couldn't import by itself.
    static let importHint = "On your Kobo, open the menu and tap Import new books to add them to your library."

    static func format(_ name: String) -> String {
        name.lowercased().hasSuffix(".kepub.epub") ? "KEPUB" : (name as NSString).pathExtension.uppercased()
    }
}

struct KoboError: LocalizedError {
    let message: String
    /// The Kobo has a PIN and the one sent was missing or wrong.
    var needsPIN = false
    var errorDescription: String? { message }
}

/// Talks to the agent's HTTP API.
struct KoboClient {
    let base: URL
    /// Sent with every request when the Kobo has a PIN.
    var pin: String?

    private static let session: URLSession = {
        let config = URLSessionConfiguration.ephemeral
        config.timeoutIntervalForRequest = 20
        return URLSession(configuration: config)
    }()

    func info() async throws -> KoboInfo {
        try await get("api/info")
    }

    func books() async throws -> [Book] {
        struct Response: Decodable { let books: [Book] }
        let response: Response = try await get("api/books")
        return response.books
    }

    /// Uploads a file. The Kobo may convert and rename it, per its settings.
    func upload(_ file: URL, as name: String, progress: @escaping @Sendable (Double) -> Void) async throws -> UploadResult {
        var request = request(bookURL(name), method: "PUT")
        // Converting and looking up metadata happens before the Kobo answers.
        request.timeoutInterval = 120
        let (data, response) = try await Self.session.upload(
            for: request, fromFile: file, delegate: UploadProgress(onProgress: progress))
        try check(data, response)
        return try JSONDecoder().decode(UploadResult.self, from: data)
    }

    func delete(_ name: String) async throws {
        let (data, response) = try await Self.session.data(for: request(bookURL(name), method: "DELETE"))
        try check(data, response)
    }

    /// Asks the Kobo to import new books. Returns false if it couldn't.
    func rescan() async -> Bool {
        struct Response: Decodable { let ok: Bool }
        guard let (data, _) = try? await Self.session.data(for: request(base.appending(path: "api/rescan"), method: "POST")),
              let response = try? JSONDecoder().decode(Response.self, from: data)
        else { return false }
        return response.ok
    }

    func reading() async throws -> ReadingSummary {
        try await get("api/reading")
    }

    func book(_ id: String) async throws -> BookDetail {
        var components = URLComponents(url: base.appending(path: "api/book"), resolvingAgainstBaseURL: false)!
        components.queryItems = [URLQueryItem(name: "id", value: id)]
        let (data, response) = try await Self.session.data(for: request(components.url!, method: "GET"))
        try check(data, response)
        return try JSONDecoder().decode(BookDetail.self, from: data)
    }

    func coverData(_ id: String) async throws -> Data {
        var components = URLComponents(url: base.appending(path: "api/cover"), resolvingAgainstBaseURL: false)!
        components.queryItems = [URLQueryItem(name: "id", value: id)]
        let (data, response) = try await Self.session.data(for: request(components.url!, method: "GET"))
        try check(data, response)
        return data
    }

    func settings() async throws -> KoboSettings {
        struct Response: Decodable { let settings: KoboSettings }
        let response: Response = try await get("api/settings")
        return response.settings
    }

    func save(_ settings: KoboSettings) async throws {
        try await send("api/settings", method: "PUT", body: settings)
    }

    /// Replaces the list of books hidden from the reading lists.
    func setHidden(_ ids: [String]) async throws {
        try await send("api/settings", method: "PUT", body: ["hidden": ids])
    }

    /// Stores (or with "" removes) a secret setting: "googleApiKey" or "hardcoverToken".
    func setSecret(_ field: String, to value: String) async throws {
        try await send("api/settings", method: "PUT", body: [field: value])
    }

    /// Sets or changes the PIN. Requests must already carry the current PIN, if any.
    func setPIN(_ newPIN: String) async throws {
        try await send("api/pin", method: "PUT", body: ["pin": newPIN])
    }

    func removePIN() async throws {
        let (data, response) = try await Self.session.data(for: request(base.appending(path: "api/pin"), method: "DELETE"))
        try check(data, response)
    }

    private func get<T: Decodable>(_ path: String) async throws -> T {
        let (data, response) = try await Self.session.data(for: request(base.appending(path: path), method: "GET"))
        try check(data, response)
        return try JSONDecoder().decode(T.self, from: data)
    }

    private func send(_ path: String, method: String, body: some Encodable) async throws {
        var request = request(base.appending(path: path), method: method)
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(body)
        let (data, response) = try await Self.session.data(for: request)
        try check(data, response)
    }

    private func request(_ url: URL, method: String) -> URLRequest {
        var request = URLRequest(url: url)
        request.httpMethod = method
        if let pin { request.setValue(pin, forHTTPHeaderField: "X-EzKobo-PIN") }
        return request
    }

    private func bookURL(_ name: String) -> URL {
        var allowed = CharacterSet.urlPathAllowed
        allowed.remove("/")
        let encoded = name.addingPercentEncoding(withAllowedCharacters: allowed) ?? name
        return URL(string: base.absoluteString + "/api/books/" + encoded)!
    }

    private func check(_ data: Data, _ response: URLResponse) throws {
        guard let http = response as? HTTPURLResponse, http.statusCode >= 300 else { return }
        struct Failure: Decodable { let error: String }
        let message = (try? JSONDecoder().decode(Failure.self, from: data))?.error
        throw KoboError(message: message ?? "The Kobo returned an error (\(http.statusCode)).",
                        needsPIN: http.statusCode == 401)
    }
}

private final class UploadProgress: NSObject, URLSessionTaskDelegate {
    let onProgress: @Sendable (Double) -> Void

    init(onProgress: @escaping @Sendable (Double) -> Void) {
        self.onProgress = onProgress
    }

    func urlSession(_ session: URLSession, task: URLSessionTask, didSendBodyData bytesSent: Int64,
                    totalBytesSent: Int64, totalBytesExpectedToSend: Int64) {
        if totalBytesExpectedToSend > 0 {
            onProgress(Double(totalBytesSent) / Double(totalBytesExpectedToSend))
        }
    }
}
