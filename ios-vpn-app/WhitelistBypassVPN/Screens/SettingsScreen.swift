import SwiftUI

enum SettingsSheet: Identifiable {
    case theme, tunnelMode, vp8, autofill, proxy, dns, resetAll, forgetAll
    var id: String { String(describing: self) }
}

struct SettingsScreen: View {
    @EnvironmentObject var appState: AppState
    @State private var sheet: SettingsSheet?

    var body: some View {
        ScrollView {
            VStack(spacing: 0) {
                SettingsSection(title: NSLocalizedString("settings_section_appearance", comment: "")) {
                    navRow(icon: "paintbrush", title: NSLocalizedString("settings_row_theme", comment: ""),
                           sub: NSLocalizedString("settings_row_theme_sub", comment: ""),
                           value: appState.themeMode.label) { sheet = .theme }
                }

                SettingsSection(title: NSLocalizedString("settings_section_tunnel", comment: "")) {
                    navRow(icon: "arrow.triangle.branch", title: NSLocalizedString("settings_row_tunnel_mode", comment: ""),
                           value: appState.tunnelMode.label) { sheet = .tunnelMode }
                    Divider().overlay(Palette.hair)
                    navRow(icon: "square.stack.3d.up", title: NSLocalizedString("settings_row_vp8", comment: ""),
                           sub: vp8Summary) { sheet = .vp8 }
                    Divider().overlay(Palette.hair)
                    navRow(icon: "textformat", title: NSLocalizedString("settings_row_autofill", comment: ""),
                           sub: autofillSummary) { sheet = .autofill }
                }

                SettingsSection(title: NSLocalizedString("settings_section_network", comment: "")) {
                    navRow(icon: "network", title: NSLocalizedString("settings_row_proxy", comment: ""),
                           sub: String(format: NSLocalizedString("settings_row_proxy_sub", comment: ""), appState.socksPort)) { sheet = .proxy }
                    Divider().overlay(Palette.hair)
                    navRow(icon: "globe", title: NSLocalizedString("settings_row_dns", comment: ""),
                           value: appState.dnsMode.label) { sheet = .dns }
                }

                SettingsSection(title: NSLocalizedString("settings_section_behavior", comment: "")) {
                    SettingsRow(icon: "arrow.clockwise", title: NSLocalizedString("settings_row_reconnect", comment: ""),
                                sub: NSLocalizedString("settings_row_reconnect_sub", comment: "")) {
                        Toggle("", isOn: $appState.connectOnStart).labelsHidden().tint(Palette.accent)
                    }
                    Divider().overlay(Palette.hair)
                    SettingsRow(icon: "ladybug", title: NSLocalizedString("settings_row_debug", comment: ""),
                                sub: NSLocalizedString("settings_row_debug_sub", comment: "")) {
                        Toggle("", isOn: $appState.debug).labelsHidden().tint(Palette.accent)
                    }
                }

                SettingsSection(title: NSLocalizedString("settings_section_danger", comment: "")) {
                    navRow(icon: "arrow.counterclockwise", title: NSLocalizedString("settings_reset_all", comment: ""),
                           sub: NSLocalizedString("settings_reset_all_sub", comment: ""), danger: true) { sheet = .resetAll }
                    Divider().overlay(Palette.hair)
                    navRow(icon: "trash", title: NSLocalizedString("settings_forget_all_destinations", comment: ""),
                           sub: NSLocalizedString("settings_forget_all_destinations_sub", comment: ""), danger: true) { sheet = .forgetAll }
                }
            }
            .padding(.horizontal, 16)
            .padding(.bottom, 24)
        }
        .sheet(item: $sheet) { sheetView($0) }
    }

    private func navRow(icon: String, title: String, sub: String? = nil, value: String? = nil, danger: Bool = false, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            SettingsRow(icon: icon, title: title, sub: sub, danger: danger) {
                HStack(spacing: 6) {
                    if let value {
                        Text(value).font(Mono.label(11)).foregroundStyle(Palette.ink3)
                    }
                    Image(systemName: "chevron.right").font(.system(size: 11)).foregroundStyle(Palette.ink3)
                }
            }
        }
        .buttonStyle(.plain)
    }

    @ViewBuilder
    private func sheetView(_ sheet: SettingsSheet) -> some View {
        switch sheet {
        case .theme:
            ChoiceSheet(title: NSLocalizedString("settings_row_theme", comment: ""),
                        options: ThemeMode.allCases.map { ChoiceOption(id: $0.rawValue, title: $0.label) },
                        selectedId: appState.themeMode.rawValue) { rawValue in
                if let mode = ThemeMode(rawValue: rawValue) { appState.themeMode = mode }
            }
        case .tunnelMode:
            ChoiceSheet(title: NSLocalizedString("settings_row_tunnel_mode", comment: ""),
                        options: TunnelMode.allCases.map { ChoiceOption(id: $0.rawValue, title: $0.label) },
                        selectedId: appState.tunnelMode.rawValue) { rawValue in
                if let mode = TunnelMode(rawValue: rawValue) { appState.tunnelMode = mode }
            }
        case .vp8:
            Vp8Sheet(fps: appState.vp8Fps, batch: appState.vp8Batch,
                     dualTrack: appState.dualTrack, reliable: appState.reliable) { fps, batch, dual, reliable in
                appState.vp8Fps = fps
                appState.vp8Batch = batch
                appState.dualTrack = dual
                appState.reliable = reliable
            }
        case .autofill:
            AutofillSheet(appState: appState)
        case .proxy:
            ProxySheet(appState: appState)
        case .dns:
            DnsSheet(appState: appState)
        case .resetAll:
            ConfirmSheet(title: NSLocalizedString("settings_reset_all", comment: ""),
                         sub: NSLocalizedString("settings_reset_all_sub", comment: ""),
                         confirmTitle: NSLocalizedString("confirm_reset", comment: "")) {
                appState.resetAllSettings()
            }
        case .forgetAll:
            ConfirmSheet(title: NSLocalizedString("settings_forget_all_destinations", comment: ""),
                         sub: NSLocalizedString("settings_forget_all_destinations_sub", comment: ""),
                         confirmTitle: NSLocalizedString("confirm_forget", comment: "")) {
                appState.forgetAllCalls()
            }
        }
    }

    private var vp8Summary: String {
        var summary = String(format: NSLocalizedString("settings_row_vp8_sub", comment: ""), appState.vp8Fps, appState.vp8Batch)
        if appState.dualTrack { summary += " / " + NSLocalizedString("settings_row_vp8_flag_dual", comment: "") }
        if appState.reliable { summary += " / " + NSLocalizedString("settings_row_vp8_flag_kcp", comment: "") }
        return summary
    }

    private var autofillSummary: String {
        appState.autofillEnabled ? appState.autofillName : NSLocalizedString("settings_row_autofill_off", comment: "")
    }
}

struct SettingsSection<Content: View>: View {
    let title: String
    @ViewBuilder var content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(title.uppercased())
                .font(Mono.label(10))
                .tracking(1.8)
                .foregroundStyle(Palette.ink3)
                .padding(.horizontal, 8)
                .padding(.bottom, 8)

            VStack(spacing: 0) {
                content
            }
            .statCard()
        }
        .padding(.top, 20)
    }
}

struct SettingsRow<Trailing: View>: View {
    let icon: String
    let title: String
    var sub: String? = nil
    var danger: Bool = false
    @ViewBuilder var trailing: Trailing

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: icon)
                .font(.system(size: 13))
                .foregroundStyle(danger ? Palette.error : Palette.ink2)
                .frame(width: 28, height: 28)
                .background(
                    RoundedRectangle(cornerRadius: 9, style: .continuous)
                        .fill(danger ? Palette.error.opacity(0.12) : Palette.surface)
                )
                .overlay(
                    RoundedRectangle(cornerRadius: 9, style: .continuous)
                        .stroke(Palette.hair, lineWidth: 1)
                )

            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.system(size: 13, weight: .bold))
                    .foregroundStyle(danger ? Palette.error : Palette.ink)
                if let sub {
                    Text(sub)
                        .font(.system(size: 11))
                        .foregroundStyle(Palette.ink3)
                }
            }

            Spacer(minLength: 8)

            trailing
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
    }
}
