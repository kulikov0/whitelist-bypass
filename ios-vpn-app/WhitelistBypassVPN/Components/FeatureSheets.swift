import SwiftUI

private func clampedInt(_ text: String, lo: Int, hi: Int, fallback: Int) -> Int {
    guard let value = Int(text.trimmingCharacters(in: .whitespaces)) else { return fallback }
    return min(max(value, lo), hi)
}

struct AddDestinationSheet: View {
    @ObservedObject var appState: AppState
    @State private var name: String
    @State private var link: String
    @State private var pasteFlashed = false
    @Environment(\.dismiss) private var dismiss

    init(appState: AppState, prefillUrl: String = "") {
        self.appState = appState
        let url = prefillUrl.trimmingCharacters(in: .whitespacesAndNewlines)
        _link = State(initialValue: url)
        _name = State(initialValue: url.isEmpty ? "" : CallPlatform.detect(url: url).suggestedName)
    }

    var body: some View {
        SheetScaffold {
            SheetHeader(title: NSLocalizedString("sheet_new_title", comment: ""),
                        sub: NSLocalizedString("sheet_new_sub", comment: ""))

            FieldBlock(label: NSLocalizedString("sheet_field_name", comment: "")) {
                SheetField(placeholder: NSLocalizedString("sheet_field_name_hint", comment: ""), text: $name)
            }

            FieldBlock(label: NSLocalizedString("sheet_field_link", comment: "")) {
                SheetField(placeholder: NSLocalizedString("sheet_field_link_hint", comment: ""),
                           text: $link, mono: true, keyboard: .URL)
            }

            Button(action: pasteFromClipboard) {
                HStack(spacing: 8) {
                    Image(systemName: "doc.on.clipboard").font(.system(size: 13))
                    Text(NSLocalizedString("paste_from_clipboard", comment: ""))
                        .font(.system(size: 13, weight: .semibold))
                }
                .foregroundStyle(pasteFlashed ? Palette.accent : Palette.ink2)
                .padding(.vertical, 10)
                .padding(.horizontal, 14)
                .overlay(
                    RoundedRectangle(cornerRadius: 10, style: .continuous)
                        .stroke(pasteFlashed ? Palette.accent : Palette.hairStrong, lineWidth: 1)
                )
            }
            .buttonStyle(.plain)

            SheetFooter(saveTitle: NSLocalizedString("sheet_save", comment: ""),
                        destructive: false,
                        onCancel: { dismiss() },
                        onSave: save)
        }
    }

    private func save() {
        let trimmedLink = link.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedLink.isEmpty else { return }
        appState.addCall(name: name, url: trimmedLink)
        dismiss()
    }

    private func pasteFromClipboard() {
        guard let clip = UIPasteboard.general.string?.trimmingCharacters(in: .whitespacesAndNewlines), !clip.isEmpty else { return }
        link = clip
        if name.trimmingCharacters(in: .whitespaces).isEmpty {
            name = CallPlatform.detect(url: clip).suggestedName
        }
        withAnimation(.easeOut(duration: 0.32)) { pasteFlashed = true }
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.32) {
            withAnimation { pasteFlashed = false }
        }
    }
}

struct Vp8Sheet: View {
    let onSave: (Int, Int, Bool, Bool) -> Void
    @State private var fps: String
    @State private var batch: String
    @State private var dualTrack: Bool
    @State private var reliable: Bool
    @Environment(\.dismiss) private var dismiss

    init(fps: Int, batch: Int, dualTrack: Bool, reliable: Bool, onSave: @escaping (Int, Int, Bool, Bool) -> Void) {
        self.onSave = onSave
        _fps = State(initialValue: String(fps))
        _batch = State(initialValue: String(batch))
        _dualTrack = State(initialValue: dualTrack)
        _reliable = State(initialValue: reliable)
    }

    var body: some View {
        SheetScaffold {
            SheetHeader(title: NSLocalizedString("vp8_settings_title", comment: ""),
                        sub: NSLocalizedString("vp8_settings_sub", comment: ""))

            HStack(spacing: 12) {
                FieldBlock(label: NSLocalizedString("vp8_fps_label", comment: "")) {
                    SheetField(text: $fps, keyboard: .numberPad)
                }
                FieldBlock(label: NSLocalizedString("vp8_batch_label", comment: "")) {
                    SheetField(text: $batch, keyboard: .numberPad)
                }
            }

            SheetToggleRow(title: NSLocalizedString("vp8_dual_track_title", comment: ""),
                           sub: NSLocalizedString("vp8_dual_track_sub", comment: ""), isOn: $dualTrack)
            SheetToggleRow(title: NSLocalizedString("vp8_reliable_title", comment: ""),
                           sub: NSLocalizedString("vp8_reliable_sub", comment: ""), isOn: $reliable)

            SheetFooter(onCancel: { dismiss() }, onSave: {
                onSave(clampedInt(fps, lo: 1, hi: 240, fallback: Defaults.vp8Fps),
                       clampedInt(batch, lo: 1, hi: 256, fallback: Defaults.vp8Batch),
                       dualTrack, reliable)
                dismiss()
            })
        }
    }
}

struct DnsSheet: View {
    @ObservedObject var appState: AppState
    @State private var mode: DnsMode
    @State private var primary: String
    @State private var secondary: String
    @Environment(\.dismiss) private var dismiss

    init(appState: AppState) {
        self.appState = appState
        _mode = State(initialValue: appState.dnsMode)
        _primary = State(initialValue: appState.dnsPrimary)
        _secondary = State(initialValue: appState.dnsSecondary)
    }

    var body: some View {
        SheetScaffold {
            SheetHeader(title: NSLocalizedString("dns_settings_title", comment: ""),
                        sub: NSLocalizedString("dns_sub", comment: ""))

            VStack(spacing: 8) {
                ForEach(DnsMode.allCases) { option in
                    OptionRow(title: option.label, selected: option == mode) { mode = option }
                }
            }

            if mode == .custom {
                FieldBlock(label: NSLocalizedString("dns_primary_label", comment: "")) {
                    SheetField(placeholder: VpnConfig.dnsPrimary, text: $primary, mono: true, keyboard: .numbersAndPunctuation)
                }
                FieldBlock(label: NSLocalizedString("dns_secondary_label", comment: "")) {
                    SheetField(placeholder: VpnConfig.dnsSecondary, text: $secondary, mono: true, keyboard: .numbersAndPunctuation)
                }
            }

            SheetFooter(onCancel: { dismiss() }, onSave: {
                appState.dnsMode = mode
                appState.dnsPrimary = primary.trimmingCharacters(in: .whitespaces)
                appState.dnsSecondary = secondary.trimmingCharacters(in: .whitespaces)
                dismiss()
            })
        }
    }
}

struct ProxySheet: View {
    @ObservedObject var appState: AppState
    @State private var port: String
    @State private var authMode: SocksAuthMode
    @State private var user: String
    @State private var pass: String
    @Environment(\.dismiss) private var dismiss

    init(appState: AppState) {
        self.appState = appState
        _port = State(initialValue: String(appState.socksPort))
        _authMode = State(initialValue: appState.socksAuthMode)
        _user = State(initialValue: appState.manualSocksUser)
        _pass = State(initialValue: appState.manualSocksPass)
    }

    var body: some View {
        SheetScaffold {
            SheetHeader(title: NSLocalizedString("proxy_settings_title", comment: ""),
                        sub: NSLocalizedString("proxy_sub", comment: ""))

            FieldBlock(label: NSLocalizedString("proxy_port_field_label", comment: "")) {
                SheetField(text: $port, keyboard: .numberPad)
            }

            FieldBlock(label: NSLocalizedString("proxy_auth_field_label", comment: "")) {
                VStack(spacing: 8) {
                    ForEach(SocksAuthMode.allCases) { option in
                        OptionRow(title: option.label, sub: option.sub, selected: option == authMode) { authMode = option }
                    }
                }
            }

            if authMode == .manual {
                FieldBlock(label: NSLocalizedString("proxy_user_field_label", comment: "")) {
                    SheetField(placeholder: NSLocalizedString("proxy_username_hint", comment: ""), text: $user)
                }
                FieldBlock(label: NSLocalizedString("proxy_pass_field_label", comment: "")) {
                    SheetField(placeholder: NSLocalizedString("proxy_password_hint", comment: ""), text: $pass, secure: true)
                }
            }

            SheetFooter(onCancel: { dismiss() }, onSave: {
                appState.socksPort = clampedInt(port, lo: 1, hi: 65535, fallback: VpnConfig.defaultSocksPort)
                appState.socksAuthMode = authMode
                appState.manualSocksUser = user
                appState.manualSocksPass = pass
                dismiss()
            })
        }
    }
}

struct AutofillSheet: View {
    @ObservedObject var appState: AppState
    @State private var enabled: Bool
    @State private var name: String
    @Environment(\.dismiss) private var dismiss

    init(appState: AppState) {
        self.appState = appState
        _enabled = State(initialValue: appState.autofillEnabled)
        _name = State(initialValue: appState.autofillName)
    }

    var body: some View {
        SheetScaffold {
            SheetHeader(title: NSLocalizedString("autofill_settings_title", comment: ""),
                        sub: NSLocalizedString("autofill_settings_sub", comment: ""))

            SheetToggleRow(title: NSLocalizedString("autofill_enable_title", comment: ""),
                           sub: NSLocalizedString("autofill_enable_sub", comment: ""), isOn: $enabled)

            FieldBlock(label: NSLocalizedString("autofill_name_field_label", comment: "")) {
                HStack(spacing: 8) {
                    SheetField(placeholder: NSLocalizedString("autofill_name_input_hint", comment: ""), text: $name)
                    Button {
                        name = RandomNames.pool.randomElement() ?? Defaults.autofillName
                    } label: {
                        Text(NSLocalizedString("autofill_generate_random", comment: ""))
                            .font(.system(size: 13, weight: .semibold))
                            .foregroundStyle(Palette.accent)
                            .padding(.vertical, 12)
                            .padding(.horizontal, 14)
                            .overlay(
                                RoundedRectangle(cornerRadius: 12, style: .continuous)
                                    .stroke(Palette.hairStrong, lineWidth: 1)
                            )
                    }
                    .buttonStyle(.plain)
                }
            }

            SheetFooter(onCancel: { dismiss() }, onSave: {
                appState.autofillEnabled = enabled
                let trimmed = name.trimmingCharacters(in: .whitespaces)
                appState.autofillName = trimmed.isEmpty ? Defaults.autofillName : trimmed
                dismiss()
            })
        }
    }
}
