import SwiftUI

struct CallRow: View {
    let call: CallConfig
    let active: Bool
    let phase: ConnectionPhase
    let onTap: () -> Void
    let onLong: () -> Void

    var body: some View {
        HStack(spacing: 12) {
            Circle().fill(dotColor).frame(width: 6, height: 6)

            Text(call.glyph)
                .font(Mono.bold(12))
                .foregroundStyle(active ? .white : Palette.ink2)
                .frame(width: 40, height: 40)
                .background(
                    RoundedRectangle(cornerRadius: 11, style: .continuous)
                        .fill(active ? Palette.accent : Palette.surface)
                )
                .overlay(
                    RoundedRectangle(cornerRadius: 11, style: .continuous)
                        .stroke(Palette.hair, lineWidth: 1)
                )

            VStack(alignment: .leading, spacing: 2) {
                Text(call.name)
                    .font(.system(size: 14, weight: .bold))
                    .foregroundStyle(Palette.ink)
                    .lineLimit(1)
                Text(call.url)
                    .font(Mono.label(11))
                    .foregroundStyle(Palette.ink3)
                    .lineLimit(1)
            }

            Spacer(minLength: 8)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .frame(maxWidth: .infinity)
        .background(
            RoundedRectangle(cornerRadius: 16, style: .continuous)
                .fill(active ? Palette.accentSoft : Palette.panel2)
        )
        .overlay(
            RoundedRectangle(cornerRadius: 16, style: .continuous)
                .stroke(active ? Palette.accent.opacity(0.4) : Palette.hair, lineWidth: 1)
        )
        .contentShape(Rectangle())
        .onTapGesture { onTap() }
        .onLongPressGesture(minimumDuration: 0.4) { onLong() }
    }

    private var dotColor: Color {
        guard active else { return Palette.ink3 }
        switch phase {
        case .connected: return Palette.accent
        case .connecting: return Palette.warn
        default: return Palette.ink2
        }
    }
}
