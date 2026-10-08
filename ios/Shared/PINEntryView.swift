import SwiftUI

/// Asks for a PIN. `submit` returns an error message, or nil on success.
struct PINEntryView: View {
    let title: String
    let message: String
    let button: String
    let submit: (String) async -> String?

    @Environment(\.dismiss) private var dismiss
    @State private var pin = ""
    @State private var error: String?
    @State private var working = false
    @FocusState private var focused: Bool

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    SecureField("PIN", text: $pin)
                        .keyboardType(.numberPad)
                        .textContentType(.oneTimeCode)
                        .focused($focused)
                } footer: {
                    Text(error ?? message)
                        .foregroundStyle(error == nil ? Color.secondary : Color.red)
                }
            }
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel", role: .cancel) { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(button) {
                        Task {
                            working = true
                            error = await submit(pin)
                            working = false
                            if error == nil { dismiss() }
                        }
                    }
                    .disabled(pin.count < 4 || working)
                }
            }
            .onAppear { focused = true }
        }
        .presentationDetents([.medium])
    }
}
