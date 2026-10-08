import SwiftUI
import UIKit

/// A book in the Kobo's library, with what the Kobo knows about reading it.
struct ReadingBook: Decodable, Identifiable, Hashable {
    let id: String
    let title: String
    let author: String?
    let series: String?
    let seriesNumber: String?
    /// 0 not started, 1 reading, 2 finished.
    let status: Int
    let progress: Int
    let secondsRead: Int?
    let secondsLeft: Int?
    let lastRead: String?
    let finished: String?
    let timesStarted: Int?
    let pages: Int?
    let words: Int?
    let publisher: String?
    let isbn: String?
    let description: String?
    let cover: Bool

    var lastReadDate: Date? { KoboDate.parse(lastRead) }
    var finishedDate: Date? { KoboDate.parse(finished) }
    var seriesLabel: String? {
        guard let series, !series.isEmpty else { return nil }
        guard let n = seriesNumber, !n.isEmpty else { return series }
        return "\(series) #\(n)"
    }
}

struct ReadingStats: Decodable, Hashable {
    let books: Int
    let reading: Int
    let finished: Int
    let notStarted: Int
    let secondsRead: Int
}

struct ReadingSummary: Decodable {
    let reading: [ReadingBook]
    let finished: [ReadingBook]
    let byTime: [ReadingBook]
    let stats: ReadingStats
}

struct Highlight: Decodable, Hashable, Identifiable {
    let text: String?
    let note: String?
    let created: String?
    let type: String
    /// Where it is in the book, 0–100 (Kobo has no page numbers for sideloaded books).
    let position: Double?

    var positionLabel: String? { position.map { "\(Int($0.rounded()))%" } }
    var id: String { (created ?? "") + (text ?? "") + (note ?? "") }
}

struct BookDetail: Decodable {
    let book: ReadingBook
    let highlights: [Highlight]
}

enum KoboDate {
    private static let plain: ISO8601DateFormatter = ISO8601DateFormatter()
    private static let fractional: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f
    }()

    /// The Kobo stores dates as ISO 8601, sometimes with fractions or no zone.
    static func parse(_ s: String?) -> Date? {
        guard var s, !s.isEmpty else { return nil }
        if let d = plain.date(from: s) ?? fractional.date(from: s) { return d }
        if !s.hasSuffix("Z") { s += "Z" }
        return plain.date(from: s) ?? fractional.date(from: s)
    }
}

/// "7 hr, 30 min"
func readingTime(_ seconds: Int) -> String {
    if seconds < 60 { return "Under a minute" }
    return Duration.seconds(seconds).formatted(.units(allowed: [.hours, .minutes], width: .abbreviated))
}

/// A book cover from the Kobo's own cover cache, with a plain placeholder.
struct CoverImage: View {
    let client: KoboClient?
    let book: ReadingBook?
    var width: CGFloat = 40

    @State private var image: UIImage?

    var body: some View {
        ZStack {
            if let image {
                Image(uiImage: image).resizable().scaledToFill()
            } else {
                Rectangle().fill(.quaternary)
                    .overlay(Image(systemName: "book.closed").foregroundStyle(.secondary))
            }
        }
        .frame(width: width, height: width * 1.5)
        .clipShape(RoundedRectangle(cornerRadius: width > 60 ? 6 : 3, style: .continuous))
        .accessibilityHidden(true)
        .task(id: book?.id) { await load() }
    }

    private func load() async {
        guard let client, let book, book.cover else { return }
        if let cached = CoverCache.shared.object(forKey: book.id as NSString) {
            image = cached
            return
        }
        if let data = try? await client.coverData(book.id), let loaded = UIImage(data: data) {
            CoverCache.shared.setObject(loaded, forKey: book.id as NSString)
            image = loaded
        }
    }
}

enum CoverCache {
    static let shared = NSCache<NSString, UIImage>()
}
