import SwiftUI

struct LogsScreen: View {
    @EnvironmentObject var vpn: VPNController

    var body: some View {
        VStack(spacing: 0) {
            if vpn.logs.isEmpty {
                Spacer()
                Text(NSLocalizedString("logs_empty", comment: ""))
                    .font(Mono.label(12))
                    .foregroundStyle(Palette.ink3)
                Spacer()
            } else {
                ScrollViewReader { proxy in
                    List {
                        ForEach(vpn.logs.indices, id: \.self) { index in
                            LogLine(text: vpn.logs[index])
                                .id(index)
                                .listRowInsets(EdgeInsets())
                                .listRowBackground(Color.clear)
                                .listRowSeparatorTint(Palette.hair)
                        }
                    }
                    .listStyle(.plain)
                    .scrollContentBackground(.hidden)
                    .onChange(of: vpn.logs.count) { count in
                        if count > 0 { proxy.scrollTo(count - 1, anchor: .bottom) }
                    }
                }
            }

            Rectangle().fill(Palette.hair).frame(height: 1)

            HStack(spacing: 8) {
                Button {
                    UIPasteboard.general.string = rawLog
                } label: {
                    logButton(icon: "doc.on.doc", title: NSLocalizedString("logs_copy", comment: ""))
                }
                .buttonStyle(.plain)

                if let url = relayLogFile {
                    ShareLink(item: url) {
                        logButton(icon: "square.and.arrow.up", title: NSLocalizedString("logs_share", comment: ""))
                    }
                } else {
                    ShareLink(item: rawLog) {
                        logButton(icon: "square.and.arrow.up", title: NSLocalizedString("logs_share", comment: ""))
                    }
                }
            }
            .padding(.horizontal, 20)
            .padding(.top, 12)
            .padding(.bottom, 20)
        }
    }

    private var relayLogFile: URL? {
        guard let url = AppIdentifiers.relayLogURL, FileManager.default.fileExists(atPath: url.path) else { return nil }
        return url
    }

    private var rawLog: String {
        if let url = relayLogFile, let text = try? String(contentsOf: url, encoding: .utf8), !text.isEmpty {
            return text
        }
        return vpn.logs.joined(separator: "\n")
    }

    private func logButton(icon: String, title: String) -> some View {
        HStack(spacing: 8) {
            Image(systemName: icon).font(.system(size: 14))
            Text(title).font(.system(size: 13, weight: .semibold))
        }
        .foregroundStyle(Palette.ink)
        .frame(maxWidth: .infinity)
        .padding(.vertical, 11)
        .overlay(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .stroke(Palette.hairStrong, lineWidth: 1)
        )
    }
}

struct LogLine: View {
    let text: String

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: iconName)
                .font(.system(size: 12))
                .foregroundStyle(iconColor)
                .frame(width: 26, height: 26)
                .background(
                    RoundedRectangle(cornerRadius: 9, style: .continuous)
                        .fill(Palette.surface)
                )
                .overlay(
                    RoundedRectangle(cornerRadius: 9, style: .continuous)
                        .stroke(Palette.hair, lineWidth: 1)
                )

            Text(text)
                .font(Mono.label(11))
                .foregroundStyle(Palette.ink)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
    }

    private var lower: String { text.lowercased() }

    private var iconName: String {
        if lower.contains("error") || lower.contains("failed") { return "exclamationmark.triangle" }
        if lower.contains("connected") || lower.contains("tunnel active") { return "bolt.fill" }
        if lower.contains("captcha") { return "checkmark.shield" }
        if lower.contains("socks") { return "network" }
        return "info.circle"
    }

    private var iconColor: Color {
        if lower.contains("error") || lower.contains("failed") { return Palette.error }
        if lower.contains("connected") || lower.contains("tunnel active") { return Palette.accent }
        return Palette.ink2
    }
}
