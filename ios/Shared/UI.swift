import SwiftUI

/// A small line drawing of the e-reader, used like an icon in Kobo rows.
/// Libra-style models get the side bezel with page-turn buttons, so the two
/// kinds are easy to tell apart at a glance.
struct DeviceGlyph: View {
    let model: String

    private var hasButtons: Bool {
        ["Libra", "Forma", "Sage"].contains { model.localizedCaseInsensitiveContains($0) }
    }

    var body: some View {
        ZStack(alignment: .trailing) {
            RoundedRectangle(cornerRadius: 5, style: .continuous)
                .strokeBorder(.secondary, lineWidth: 1.5)
            RoundedRectangle(cornerRadius: 1.5, style: .continuous)
                .fill(.quaternary)
                .padding(.leading, 3.5)
                .padding(.vertical, 4)
                .padding(.trailing, hasButtons ? 9 : 3.5)
            if hasButtons {
                VStack(spacing: 2.5) {
                    Capsule().frame(width: 1.5, height: 5)
                    Capsule().frame(width: 1.5, height: 5)
                }
                .foregroundStyle(.secondary)
                .padding(.trailing, 3.5)
            }
        }
        .frame(width: hasButtons ? 31 : 27, height: 38)
        .frame(width: 34)
        .accessibilityHidden(true)
    }
}

/// A Kobo with its details, as a list row.
struct KoboRow: View {
    let kobo: Kobo
    let info: KoboInfo?
    let reachable: Bool
    var selected = false

    var body: some View {
        HStack(spacing: 14) {
            DeviceGlyph(model: info?.model ?? kobo.model)
            VStack(alignment: .leading, spacing: 3) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(info?.displayName.flatMap { $0.isEmpty ? nil : $0 } ?? info?.model ?? kobo.model)
                        .foregroundStyle(.primary)
                    if let serial = info?.serial, !serial.isEmpty {
                        Text(serial)
                            .font(.footnote.monospaced())
                            .foregroundStyle(.secondary)
                    }
                    if info?.locked == true {
                        Image(systemName: "lock.fill")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .accessibilityLabel("PIN protected")
                    }
                }
                details
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
            if selected {
                Image(systemName: "checkmark")
                    .fontWeight(.semibold)
                    .foregroundStyle(Color.accentColor)
            }
        }
        .padding(.vertical, 4)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    @ViewBuilder private var details: some View {
        if !reachable {
            Text("Not responding — it may be asleep")
        } else if let info {
            HStack(spacing: 10) {
                if let battery = info.battery {
                    Label("\(battery.level)%", systemImage: batterySymbol(battery))
                        .labelStyle(CompactLabel())
                }
                Text("\(ByteCountFormatter.string(fromByteCount: info.free, countStyle: .file)) free")
                Text(info.books == 1 ? "1 book" : "\(info.books) books")
            }
            .lineLimit(1)
        } else {
            Text("Connecting…")
        }
    }

    private func batterySymbol(_ battery: KoboInfo.Battery) -> String {
        if battery.charging { return "battery.100percent.bolt" }
        switch battery.level {
        case 88...: return "battery.100percent"
        case 63...: return "battery.75percent"
        case 38...: return "battery.50percent"
        case 13...: return "battery.25percent"
        default: return "battery.0percent"
        }
    }
}

private struct CompactLabel: LabelStyle {
    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: 3) {
            configuration.icon
            configuration.title
        }
    }
}

/// One file being sent, as a list row.
struct TransferRow: View {
    let item: TransferQueue.Item

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 10) {
                Text(BookName.title(item.name))
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer(minLength: 8)
                status
            }
            if item.state == .sending || item.state == .waiting {
                ProgressView(value: item.progress)
            }
            if let note = item.note {
                Text(note)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            if case let .failed(message) = item.state {
                Text(message)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 2)
    }

    @ViewBuilder private var status: some View {
        switch item.state {
        case .waiting:
            Text("Waiting").foregroundStyle(.secondary)
        case .sending:
            Text(item.progress, format: .percent.precision(.fractionLength(0)))
                .monospacedDigit()
                .foregroundStyle(.secondary)
        case .processing:
            HStack(spacing: 6) {
                ProgressView().controlSize(.small)
                Text("Processing").foregroundStyle(.secondary)
            }
        case .sent:
            Image(systemName: "checkmark.circle.fill").foregroundStyle(.green)
        case .skipped:
            Text("Already on Kobo").foregroundStyle(.secondary)
        case .failed:
            Image(systemName: "exclamationmark.circle.fill").foregroundStyle(.orange)
        }
    }
}
