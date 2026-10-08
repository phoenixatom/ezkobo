import SwiftUI

/// Every book on the Kobo: search, open details, swipe to delete.
struct LibraryView: View {
    let client: KoboClient?
    let model: String
    let books: [Book]
    let delete: (Book) async -> Void
    let reload: () async -> Void

    @State private var query = ""

    private var filtered: [Book] {
        query.isEmpty ? books : books.filter {
            $0.displayTitle.localizedStandardContains(query) || ($0.author ?? "").localizedStandardContains(query)
        }
    }

    var body: some View {
        List {
            Section {
                if books.isEmpty {
                    Text("Books you send will appear here.")
                        .foregroundStyle(.secondary)
                } else if filtered.isEmpty {
                    Text("No books match “\(query)”.")
                        .foregroundStyle(.secondary)
                }
                ForEach(filtered, id: \.listID) { book in
                    row(book)
                        .swipeActions {
                            Button("Delete", systemImage: "trash", role: .destructive) {
                                Task { await delete(book) }
                            }
                        }
                }
            } footer: {
                if books.contains(where: { $0.inLibrary == false }) {
                    Text(BookName.importHint)
                }
            }
        }
        .navigationTitle("Library")
        .navigationSubtitle(model)
        .searchable(text: $query, prompt: "Search Books")
        .refreshable { await reload() }
    }

    @ViewBuilder private func row(_ book: Book) -> some View {
        if let id = book.id, book.inLibrary == true {
            NavigationLink {
                BookDetailView(client: client, bookID: id, fallbackTitle: book.displayTitle)
            } label: {
                BookRow(book: book)
            }
        } else {
            BookRow(book: book)
        }
    }
}

struct BookRow: View {
    let book: Book

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(book.displayTitle)
                .lineLimit(2)
            Text(details)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .lineLimit(1)
            if book.inLibrary == false {
                Text("Not in library yet")
                    .font(.subheadline)
                    .foregroundStyle(.orange)
            }
        }
        .padding(.vertical, 2)
    }

    private var details: String {
        var parts: [String] = []
        if let author = book.author, !author.isEmpty { parts.append(author) }
        parts.append(BookName.format(book.name))
        if let progress = book.progress, progress > 0 {
            parts.append("\(progress)% read")
        } else {
            parts.append(ByteCountFormatter.string(fromByteCount: book.size, countStyle: .file))
        }
        return parts.joined(separator: " · ")
    }
}
