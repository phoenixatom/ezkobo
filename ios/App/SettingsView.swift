import SwiftUI

/// Settings for one Kobo. They're stored on the Kobo, so they apply to
/// books sent from the app, the Share sheet and the browser alike.
struct SettingsView: View {
    let kobo: Kobo
    let model: String
    let finder: KoboFinder
    @Environment(\.dismiss) private var dismiss

    @State private var settings: KoboSettings?
    @State private var hasPIN = false
    @State private var error: String?
    @State private var pinSheet: PINAction?
    @State private var confirmRemovePIN = false

    enum PINAction: String, Identifiable {
        case set, change
        var id: String { rawValue }
    }

    var body: some View {
        NavigationStack {
            Form {
                if let error {
                    Section {
                        Label(error, systemImage: "exclamationmark.triangle")
                            .foregroundStyle(.secondary)
                    }
                }
                if let settings {
                    arrivalSection(settings)
                    pinSection
                } else if error == nil {
                    ProgressView().frame(maxWidth: .infinity)
                }
            }
            .navigationTitle(model)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
            .task { await load() }
            .sheet(item: $pinSheet) { action in
                PINEntryView(title: action == .set ? "Set PIN" : "Change PIN",
                             message: "4 to 8 digits. Phones and browsers will need it to see or send books.",
                             button: "Save") { newPIN in
                    await savePIN(newPIN)
                }
            }
            .confirmationDialog("Turn off the PIN?", isPresented: $confirmRemovePIN, titleVisibility: .visible) {
                Button("Turn Off PIN", role: .destructive) { Task { await removePIN() } }
            } message: {
                Text("Anyone on your Wi‑Fi will be able to see, send and delete books on this Kobo.")
            }
        }
    }

    private func arrivalSection(_ current: KoboSettings) -> some View {
        Section {
            Toggle("Convert to KEPUB", isOn: binding(\.kepub))
            Toggle("Fix Missing Details", isOn: binding(\.metadata))
            Toggle("Rename as Author – Title", isOn: binding(\.cleanNames))
        } header: {
            Text("When Books Arrive")
        } footer: {
            Text("KEPUB is Kobo’s own format: faster page turns and reading stats. Fix Missing Details looks up the title, author and cover on Open Library or Google Books when a book’s own details are missing or messy, which sends the title and author to those services.")
        }
    }

    private var pinSection: some View {
        Section {
            if hasPIN {
                Button("Change PIN…") { pinSheet = .change }
                Button("Turn Off PIN", role: .destructive) { confirmRemovePIN = true }
            } else {
                Button("Set PIN…") { pinSheet = .set }
            }
        } header: {
            Text("PIN")
        } footer: {
            Text(hasPIN
                 ? "If you forget the PIN, open EzKobo status in the Kobo’s menu to see it, or tap EzKobo remove PIN."
                 : "Optional. Without a PIN, anyone on your Wi‑Fi can see, send and delete books on this Kobo.")
        }
    }

    private func binding(_ key: WritableKeyPath<KoboSettings, Bool>) -> Binding<Bool> {
        Binding {
            settings?[keyPath: key] ?? false
        } set: { value in
            settings?[keyPath: key] = value
            Task { await save() }
        }
    }

    private func load() async {
        guard let client = await finder.client(for: kobo) else {
            error = "Couldn’t reach this Kobo."
            return
        }
        do {
            settings = try await client.settings()
            hasPIN = finder.info[kobo.id]?.locked == true
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func save() async {
        guard let settings, let client = await finder.client(for: kobo) else { return }
        do {
            try await client.save(settings)
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func savePIN(_ newPIN: String) async -> String? {
        guard let client = await finder.client(for: kobo) else { return "Couldn’t reach this Kobo." }
        do {
            try await client.setPIN(newPIN)
            PINStore.save(newPIN, for: kobo.id)
            hasPIN = true
            await finder.refresh(kobo)
            return nil
        } catch {
            return error.localizedDescription
        }
    }

    private func removePIN() async {
        guard let client = await finder.client(for: kobo) else { return }
        do {
            try await client.removePIN()
            PINStore.save(nil, for: kobo.id)
            hasPIN = false
            await finder.refresh(kobo)
        } catch {
            self.error = error.localizedDescription
        }
    }
}
