import SwiftUI

/// Order and switch the services used to look up missing book details, and
/// add the keys some of them need.
struct SourcesView: View {
    @Binding var settings: KoboSettings
    let save: () async -> Void
    let setSecret: (_ field: String, _ value: String) async -> String?

    @State private var editing: Secret?

    enum Secret: String, Identifiable {
        case googleApiKey, hardcoverToken
        var id: String { rawValue }
    }

    var body: some View {
        List {
            Section {
                ForEach($settings.providers) { $provider in
                    Toggle(isOn: $provider.enabled) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(provider.name)
                            Text(detail(for: provider))
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                    }
                    .disabled(provider.id == "hardcover" && !settings.hardcoverTokenSet)
                    .onChange(of: provider.enabled) { Task { await save() } }
                }
                .onMove { from, to in
                    settings.providers.move(fromOffsets: from, toOffset: to)
                    Task { await save() }
                }
            } header: {
                Text("Order")
            } footer: {
                Text("Drag to change the order. The first source that finds the book is used.")
            }

            Section {
                keyRow("Google Books API Key", set: settings.googleApiKeySet, secret: .googleApiKey)
                keyRow("Hardcover Token", set: settings.hardcoverTokenSet, secret: .hardcoverToken)
            } header: {
                Text("Keys")
            } footer: {
                Text("Keys are stored on the Kobo and never shown again. A Google key comes from Google Cloud (Books API); a Hardcover token is under Settings › API on hardcover.app.")
            }
        }
        .environment(\.editMode, .constant(.active))
        .navigationTitle("Details Sources")
        .navigationBarTitleDisplayMode(.inline)
        .sheet(item: $editing) { secret in
            SecretEntryView(title: secret == .googleApiKey ? "Google Books API Key" : "Hardcover Token",
                            hasValue: secret == .googleApiKey ? settings.googleApiKeySet : settings.hardcoverTokenSet) { value in
                await setSecret(secret.rawValue, value)
            }
        }
    }

    private func keyRow(_ title: String, set: Bool, secret: Secret) -> some View {
        Button {
            editing = secret
        } label: {
            LabeledContent(title, value: set ? "Added" : "Not Added")
        }
        .tint(.primary)
    }

    private func detail(for provider: DetailsProvider) -> String {
        switch provider.id {
        case "apple": "Best for recent and translated books"
        case "openlibrary": "Open data; good for classics"
        case "google": settings.googleApiKeySet ? "Using your API key" : "Unreliable without an API key"
        case "hardcover": settings.hardcoverTokenSet ? "Using your token" : "Needs a token"
        default: ""
        }
    }
}

/// Adds, replaces or removes a key. `submit` returns an error message, or nil.
private struct SecretEntryView: View {
    let title: String
    let hasValue: Bool
    let submit: (String) async -> String?

    @Environment(\.dismiss) private var dismiss
    @State private var value = ""
    @State private var error: String?
    @FocusState private var focused: Bool

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    SecureField("Paste here", text: $value)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .focused($focused)
                } footer: {
                    if let error {
                        Text(error).foregroundStyle(.red)
                    }
                }
                if hasValue {
                    Section {
                        Button("Remove", role: .destructive) { run("") }
                    }
                }
            }
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel", role: .cancel) { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { run(value) }
                        .disabled(value.trimmingCharacters(in: .whitespaces).isEmpty)
                }
            }
            .onAppear { focused = true }
        }
        .presentationDetents([.medium])
    }

    private func run(_ newValue: String) {
        Task {
            error = await submit(newValue)
            if error == nil { dismiss() }
        }
    }
}
