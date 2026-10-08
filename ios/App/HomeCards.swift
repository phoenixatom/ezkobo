import SwiftUI

/// Title above a home card, like section titles in Airbnb or the App Store.
struct HomeSectionTitle: View {
    let text: String

    init(_ text: String) { self.text = text }

    var body: some View {
        Text(text)
            .font(.title3.weight(.semibold))
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityAddTraits(.isHeader)
    }
}

/// A rounded content card on the grouped background.
struct CardBackground: ViewModifier {
    func body(content: Content) -> some View {
        content
            .padding(16)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color(.secondarySystemGroupedBackground),
                        in: RoundedRectangle(cornerRadius: 26, style: .continuous))
    }
}

extension View {
    func card() -> some View { modifier(CardBackground()) }
}

/// The book being read right now, big.
struct ContinueReadingCard: View {
    let client: KoboClient?
    let book: ReadingBook

    var body: some View {
        HStack(alignment: .top, spacing: 16) {
            CoverImage(client: client, book: book, width: 92)
                .shadow(color: .black.opacity(0.15), radius: 8, y: 4)
            VStack(alignment: .leading, spacing: 6) {
                Text(book.title)
                    .font(.title3.weight(.semibold))
                    .lineLimit(3)
                    .foregroundStyle(.primary)
                if let author = book.author, !author.isEmpty {
                    Text(author)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                Spacer(minLength: 10)
                ProgressView(value: Double(book.progress), total: 100)
                    .tint(Color.accentColor)
                HStack {
                    Text("\(book.progress)%")
                        .fontWeight(.semibold)
                        .monospacedDigit()
                    if let left = book.secondsLeft, left > 0 {
                        Text("· \(readingTime(left)) left")
                            .foregroundStyle(.secondary)
                    }
                }
                .font(.subheadline)
            }
            .frame(maxWidth: .infinity, minHeight: 138, alignment: .topLeading)
        }
        .card()
        .contentShape(.rect(cornerRadius: 26))
        .background(alignment: .topTrailing) {
            // The mascot peeks over the card from behind it.
            Image("Mascot")
                .resizable()
                .scaledToFit()
                .frame(height: 94)
                .offset(x: -22, y: -56)
                .accessibilityHidden(true)
        }
    }
}

/// Other books in progress, as a horizontal shelf of covers.
struct ReadingShelf: View {
    let client: KoboClient?
    let books: [ReadingBook]
    var hide: (ReadingBook) -> Void = { _ in }

    var body: some View {
        ScrollView(.horizontal) {
            HStack(alignment: .top, spacing: 14) {
                ForEach(books) { book in
                    NavigationLink {
                        BookDetailView(client: client, bookID: book.id, fallbackTitle: book.title)
                    } label: {
                        VStack(alignment: .leading, spacing: 6) {
                            CoverImage(client: client, book: book, width: 84)
                                .shadow(color: .black.opacity(0.12), radius: 6, y: 3)
                            Text(book.title)
                                .font(.footnote.weight(.medium))
                                .lineLimit(1)
                                .foregroundStyle(.primary)
                            ProgressView(value: Double(book.progress), total: 100)
                                .tint(Color.accentColor)
                        }
                        .frame(width: 84)
                    }
                    .buttonStyle(.plain)
                    .contextMenu {
                        Button("Hide from Reading", systemImage: "eye.slash") { hide(book) }
                    }
                }
            }
            .padding(.horizontal, 20)
            .padding(.vertical, 4)
        }
        .scrollIndicators(.hidden)
        .padding(.horizontal, -20)
    }
}

/// Entry to the full library: a few covers fanned out, and the count.
struct LibraryCard: View {
    let client: KoboClient?
    let covers: [ReadingBook]
    let count: Int
    let notImported: Int

    var body: some View {
        HStack(spacing: 16) {
            ZStack(alignment: .leading) {
                ForEach(Array(covers.prefix(3).enumerated()), id: \.element.id) { index, book in
                    CoverImage(client: client, book: book, width: 40)
                        .shadow(color: .black.opacity(0.15), radius: 3, x: 1)
                        .offset(x: CGFloat(index) * 18)
                        .zIndex(Double(-index))
                }
                if covers.isEmpty {
                    Image(systemName: "books.vertical")
                        .font(.title2)
                        .foregroundStyle(.secondary)
                        .frame(width: 40, height: 60)
                }
            }
            .frame(width: covers.isEmpty ? 40 : 40 + CGFloat(min(covers.count, 3) - 1) * 18, alignment: .leading)

            VStack(alignment: .leading, spacing: 3) {
                Text("Library")
                    .font(.headline)
                    .foregroundStyle(.primary)
                Text(count == 1 ? "1 book" : "\(count) books")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                if notImported > 0 {
                    Text(notImported == 1 ? "1 not in library yet" : "\(notImported) not in library yet")
                        .font(.subheadline)
                        .foregroundStyle(.orange)
                }
            }
            Spacer(minLength: 0)
            Image(systemName: "chevron.right")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(.tertiary)
        }
        .card()
        .contentShape(.rect(cornerRadius: 26))
    }
}

/// The mascot with a short message, for empty and waiting states.
struct MascotMessage: View {
    var image = "Mascot"
    let title: String
    let message: String
    var size: CGFloat = 170
    var bounce = false

    @State private var up = false

    var body: some View {
        VStack(spacing: 14) {
            Image(image)
                .resizable()
                .scaledToFit()
                .frame(height: size)
                .offset(y: bounce && up ? -6 : 0)
                .animation(bounce ? .easeInOut(duration: 1.4).repeatForever(autoreverses: true) : nil, value: up)
                .onAppear { up = true }
                .accessibilityHidden(true)
            Text(title)
                .font(.title3.weight(.semibold))
            Text(message)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity)
        .padding(.horizontal, 24)
    }
}

/// Progress of books being sent, at the top of the home screen.
struct TransferCard: View {
    @ObservedObject var queue: TransferQueue

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text(queue.isRunning ? "Sending" : queue.failures.isEmpty ? "Sent" : "Couldn’t Send")
                    .font(.headline)
                Spacer()
                if !queue.isRunning && !queue.failures.isEmpty {
                    Button("Dismiss") { withAnimation { queue.clear() } }
                        .font(.subheadline)
                }
            }
            ForEach(queue.items) { item in
                TransferRow(item: item)
            }
            if let imported = queue.imported {
                Text(imported ? "Your Kobo is adding them to its library." : BookName.importHint)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .card()
    }
}
