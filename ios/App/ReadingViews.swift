import SwiftUI

/// A book being read: cover, title, progress and time left.
struct ReadingRow: View {
    let client: KoboClient?
    let book: ReadingBook

    var body: some View {
        HStack(spacing: 14) {
            CoverImage(client: client, book: book, width: 44)
            VStack(alignment: .leading, spacing: 4) {
                Text(book.title)
                    .lineLimit(2)
                if let author = book.author, !author.isEmpty {
                    Text(author)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                ProgressView(value: Double(book.progress), total: 100)
                Text(status)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 4)
    }

    private var status: String {
        var parts = ["\(book.progress)%"]
        if let left = book.secondsLeft, left > 0 { parts.append("\(readingTime(left)) left") }
        if let last = book.lastReadDate { parts.append(last.formatted(.relative(presentation: .named))) }
        return parts.joined(separator: " · ")
    }
}

/// Reading totals in one compact row: time read, finished, reading, to read.
struct StatsStrip: View {
    let stats: ReadingStats

    var body: some View {
        HStack(spacing: 0) {
            figure(shortTime(stats.secondsRead), "Read")
            figure("\(stats.finished)", "Finished")
            figure("\(stats.reading)", "Reading")
            figure("\(stats.notStarted)", "To Read")
        }
        .padding(.vertical, 2)
    }

    private func figure(_ value: String, _ label: String) -> some View {
        VStack(spacing: 2) {
            Text(value)
                .font(.headline)
                .monospacedDigit()
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity)
    }

    /// "23 h" or "45 min": short enough for a quarter of the row.
    private func shortTime(_ seconds: Int) -> String {
        seconds >= 3600 ? "\(Int((Double(seconds) / 3600).rounded())) h" : "\(seconds / 60) min"
    }
}

/// Everything the Kobo knows about one book, with its highlights and notes.
struct BookDetailView: View {
    let client: KoboClient?
    let bookID: String
    let fallbackTitle: String

    @State private var detail: BookDetail?
    @State private var error: String?
    @State private var descriptionExpanded = false

    var body: some View {
        List {
            if let detail {
                header(detail.book)
                progressSection(detail.book)
                if let text = detail.book.description, !text.isEmpty {
                    Section("About") {
                        Text(text)
                            .lineLimit(descriptionExpanded ? nil : 6)
                        if !descriptionExpanded && text.count > 300 {
                            Button("More") { withAnimation { descriptionExpanded = true } }
                        }
                    }
                }
                detailsSection(detail.book)
                highlightsSection(detail.highlights)
            } else if let error {
                ContentUnavailableView("Couldn’t Load Book", systemImage: "book.closed", description: Text(error))
            } else {
                ProgressView().frame(maxWidth: .infinity)
            }
        }
        .navigationTitle(detail?.book.title ?? fallbackTitle)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if let detail, !detail.highlights.isEmpty {
                ToolbarItem(placement: .topBarTrailing) {
                    ShareLink(item: export(detail), subject: Text(detail.book.title)) {
                        Label("Share Highlights", systemImage: "square.and.arrow.up")
                    }
                }
            }
        }
        .task { await load() }
    }

    private func header(_ book: ReadingBook) -> some View {
        Section {
            HStack(alignment: .top, spacing: 16) {
                CoverImage(client: client, book: book, width: 90)
                VStack(alignment: .leading, spacing: 6) {
                    Text(book.title)
                        .font(.title3.weight(.semibold))
                    if let author = book.author, !author.isEmpty {
                        Text(author).foregroundStyle(.secondary)
                    }
                    if let series = book.seriesLabel {
                        Text(series).font(.subheadline).foregroundStyle(.secondary)
                    }
                }
            }
            .padding(.vertical, 6)
        }
    }

    private func progressSection(_ book: ReadingBook) -> some View {
        Section("Reading") {
            VStack(alignment: .leading, spacing: 8) {
                ProgressView(value: Double(book.progress), total: 100)
                Text(book.status == 2 ? "Finished" : book.status == 0 ? "Not started" : "\(book.progress)% read")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            .padding(.vertical, 4)
            if let read = book.secondsRead, read > 0 {
                LabeledContent("Time Reading", value: readingTime(read))
            }
            if let left = book.secondsLeft, left > 0 {
                LabeledContent("Time Left", value: readingTime(left))
            }
            if let last = book.lastReadDate {
                LabeledContent("Last Read", value: last.formatted(date: .abbreviated, time: .shortened))
            }
            if let finished = book.finishedDate {
                LabeledContent("Finished", value: finished.formatted(date: .abbreviated, time: .omitted))
            }
            if let times = book.timesStarted, times > 1 {
                LabeledContent("Times Started", value: "\(times)")
            }
        }
    }

    @ViewBuilder private func detailsSection(_ book: ReadingBook) -> some View {
        let rows: [(String, String)] = [
            ("Pages", (book.pages ?? 0) > 0 ? "\(book.pages!)" : ""),
            ("Words", (book.words ?? 0) > 0 ? book.words!.formatted() : ""),
            ("Publisher", book.publisher ?? ""),
            ("ISBN", book.isbn ?? ""),
        ].filter { !$0.1.isEmpty }
        if !rows.isEmpty {
            Section("Details") {
                ForEach(rows, id: \.0) { row in
                    LabeledContent(row.0, value: row.1)
                }
            }
        }
    }

    @ViewBuilder private func highlightsSection(_ highlights: [Highlight]) -> some View {
        Section {
            if highlights.isEmpty {
                Text("Highlights and notes you make on the Kobo appear here.")
                    .foregroundStyle(.secondary)
            }
            ForEach(highlights) { h in
                VStack(alignment: .leading, spacing: 6) {
                    if let position = h.positionLabel {
                        Text(position)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .accessibilityLabel("\(position) through the book")
                    }
                    if let text = h.text, !text.isEmpty {
                        Text(text)
                            .padding(.leading, 10)
                            .overlay(alignment: .leading) {
                                Capsule().fill(.yellow.opacity(0.8)).frame(width: 3)
                            }
                    }
                    if let note = h.note, !note.isEmpty {
                        Label(note, systemImage: "note.text")
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                }
                .padding(.vertical, 4)
                .contextMenu {
                    Button("Copy", systemImage: "doc.on.doc") {
                        UIPasteboard.general.string = [h.text, h.note].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: "\n\n")
                    }
                }
            }
        } header: {
            Text(highlights.isEmpty ? "Highlights" : "Highlights (\(highlights.count))")
        }
    }

    /// Highlights as plain text, for sharing.
    private func export(_ detail: BookDetail) -> String {
        var lines = [detail.book.title]
        if let author = detail.book.author, !author.isEmpty { lines.append(author) }
        lines.append("")
        for h in detail.highlights {
            if let position = h.positionLabel { lines.append("[\(position)]") }
            if let text = h.text, !text.isEmpty { lines.append("“\(text)”") }
            if let note = h.note, !note.isEmpty { lines.append("Note: \(note)") }
            lines.append("")
        }
        return lines.joined(separator: "\n")
    }

    private func load() async {
        guard let client else {
            error = "Couldn’t reach this Kobo."
            return
        }
        do {
            detail = try await client.book(bookID)
        } catch {
            self.error = error.localizedDescription
        }
    }
}

/// Reading totals for one Kobo.
struct StatsView: View {
    let client: KoboClient?
    let summary: ReadingSummary
    let model: String

    var body: some View {
        List {
            Section {
                LabeledContent("Time Reading", value: readingTime(summary.stats.secondsRead))
                LabeledContent("Finished", value: "\(summary.stats.finished)")
                LabeledContent("Reading", value: "\(summary.stats.reading)")
                LabeledContent("Not Started", value: "\(summary.stats.notStarted)")
            } footer: {
                Text("From \(model)’s own reading records.")
            }
            if !summary.byTime.isEmpty {
                Section("Most Time Spent") {
                    ForEach(summary.byTime) { book in
                        link(book, detail: readingTime(book.secondsRead ?? 0))
                    }
                }
            }
            if !summary.finished.isEmpty {
                Section("Finished") {
                    ForEach(summary.finished) { book in
                        link(book, detail: book.finishedDate?.formatted(date: .abbreviated, time: .omitted) ?? "")
                    }
                }
            }
        }
        .navigationTitle("Reading Stats")
        .navigationBarTitleDisplayMode(.inline)
    }

    private func link(_ book: ReadingBook, detail: String) -> some View {
        NavigationLink {
            BookDetailView(client: client, bookID: book.id, fallbackTitle: book.title)
        } label: {
            HStack(spacing: 12) {
                CoverImage(client: client, book: book, width: 30)
                VStack(alignment: .leading, spacing: 2) {
                    Text(book.title).lineLimit(1)
                    if let author = book.author, !author.isEmpty {
                        Text(author).font(.footnote).foregroundStyle(.secondary).lineLimit(1)
                    }
                }
                Spacer(minLength: 8)
                Text(detail).font(.subheadline).foregroundStyle(.secondary)
            }
        }
    }
}
