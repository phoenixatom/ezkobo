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
}

struct Book: Decodable, Identifiable, Hashable {
    /// Path relative to the Kobo's storage, e.g. "Books/Dune.epub".
    let name: String
    let size: Int64
    let mtime: TimeInterval
    var id: String { name }
    var date: Date { Date(timeIntervalSince1970: mtime) }
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
    var errorDescription: String? { message }
}

/// Talks to the agent's HTTP API.
struct KoboClient {
    let base: URL

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

    /// Uploads a file. Returns true if the Kobo already had an identical copy.
    func upload(_ file: URL, as name: String, progress: @escaping @Sendable (Double) -> Void) async throws -> Bool {
        struct Response: Decodable { let skipped: Bool? }
        var request = URLRequest(url: bookURL(name))
        request.httpMethod = "PUT"
        let (data, response) = try await Self.session.upload(
            for: request, fromFile: file, delegate: UploadProgress(onProgress: progress))
        try check(data, response)
        return try JSONDecoder().decode(Response.self, from: data).skipped ?? false
    }

    func delete(_ name: String) async throws {
        var request = URLRequest(url: bookURL(name))
        request.httpMethod = "DELETE"
        let (data, response) = try await Self.session.data(for: request)
        try check(data, response)
    }

    /// Asks the Kobo to import new books. Returns false if it couldn't.
    func rescan() async -> Bool {
        struct Response: Decodable { let ok: Bool }
        var request = URLRequest(url: base.appending(path: "api/rescan"))
        request.httpMethod = "POST"
        guard let (data, _) = try? await Self.session.data(for: request),
              let response = try? JSONDecoder().decode(Response.self, from: data)
        else { return false }
        return response.ok
    }

    private func get<T: Decodable>(_ path: String) async throws -> T {
        let (data, response) = try await Self.session.data(from: base.appending(path: path))
        try check(data, response)
        return try JSONDecoder().decode(T.self, from: data)
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
        throw KoboError(message: message ?? "The Kobo returned an error (\(http.statusCode)).")
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
