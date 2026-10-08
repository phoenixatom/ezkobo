import Foundation
import Security

/// Remembers each Kobo's PIN in the Keychain, shared between the app and the
/// Share extension (keychain-access-groups in both targets' entitlements).
enum PINStore {
    private static let service = "EzKobo PIN"

    static func pin(for koboID: String) -> String? {
        var result: AnyObject?
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: koboID,
            kSecReturnData as String: true,
        ]
        guard SecItemCopyMatching(query as CFDictionary, &result) == errSecSuccess,
              let data = result as? Data else { return nil }
        return String(data: data, encoding: .utf8)
    }

    static func save(_ pin: String?, for koboID: String) {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: koboID,
        ]
        SecItemDelete(query as CFDictionary)
        guard let pin, let data = pin.data(using: .utf8) else { return }
        var add = query
        add[kSecValueData as String] = data
        add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
        SecItemAdd(add as CFDictionary, nil)
    }
}
