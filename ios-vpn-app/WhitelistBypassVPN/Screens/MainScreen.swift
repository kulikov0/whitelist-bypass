import SwiftUI

enum MainSheet: Identifiable {
    case add(String)
    case menu(CallConfig)
    case tunnelMode(CallConfig)
    case vp8(CallConfig)
    case rename(CallConfig)
    case delete(CallConfig)

    var id: String {
        switch self {
        case .add(let url): return "add:\(url)"
        case .menu(let c): return "menu:\(c.id)"
        case .tunnelMode(let c): return "tunnel:\(c.id)"
        case .vp8(let c): return "vp8:\(c.id)"
        case .rename(let c): return "rename:\(c.id)"
        case .delete(let c): return "delete:\(c.id)"
        }
    }
}

struct MainScreen: View {
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var vpn: VPNController
    @State private var sheet: MainSheet?
    @State private var pending: MainSheet?
    @State private var showScanner = false
    @State private var pinging = false
    @State private var pingResult: PingResult?

    private var isBusy: Bool { vpn.phase == .connected || vpn.phase == .connecting }

    private var visibleCalls: [CallConfig] {
        if isBusy, let active = appState.activeCall { return [active] }
        return appState.savedCalls
    }

    var body: some View {
        ScrollView {
            VStack(spacing: 0) {
                header
                    .padding(.top, 20)

                HeroRing(phase: vpn.phase)
                    .padding(.top, 28)
                    .onTapGesture { onHero() }

                Text(headline)
                    .font(.system(size: 22, weight: .bold))
                    .foregroundColor(Palette.ink)
                    .padding(.top, 22)

                HStack(spacing: 6) {
                    Circle().fill(dotColor).frame(width: 6, height: 6)
                    Text(vpn.statusDetail.isEmpty ? defaultDetail : vpn.statusDetail)
                        .font(Mono.label(11))
                        .foregroundColor(Palette.ink3)
                }
                .padding(.top, 6)

                if appState.savedCalls.isEmpty {
                    emptyCta.padding(.top, 24)
                } else {
                    callsList.padding(.top, 24)
                }

                if vpn.phase == .connected {
                    statsCard.padding(.top, 24)
                    pingRow.padding(.top, 12)
                }
            }
            .padding(.horizontal, 20)
            .padding(.bottom, 24)
        }
        .scrollDismissesKeyboardCompat()
        .sheet(item: $sheet, onDismiss: {
            if let next = pending { pending = nil; sheet = next }
        }) { sheetView($0) }
        .sheet(isPresented: $showScanner) {
            QRScannerSheet { scanned in
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.4) { sheet = .add(scanned) }
            }
        }
    }

    private var header: some View {
        HStack(alignment: .center, spacing: 10) {
            Text(headerSub)
                .font(Mono.label(10))
                .letterSpacing(1.2)
                .foregroundColor(Palette.ink3)
            Spacer()
            iconButton(icon: "qrcode.viewfinder") { showScanner = true }
            iconButton(icon: "plus") { sheet = .add("") }
        }
    }

    private func iconButton(icon: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: icon)
                .font(.system(size: 15))
                .foregroundColor(Palette.ink2)
                .frame(width: 36, height: 36)
                .background(
                    RoundedRectangle(cornerRadius: 10, style: .continuous)
                        .fill(Palette.panel2)
                )
                .overlay(
                    RoundedRectangle(cornerRadius: 10, style: .continuous)
                        .stroke(Palette.hair, lineWidth: 1)
                )
        }
        .buttonStyle(PlainButtonStyle())
    }

    private var callsList: some View {
        VStack(spacing: 10) {
            ForEach(visibleCalls) { call in
                CallRow(call: call, active: call.id == appState.activeCallId, phase: vpn.phase,
                        onTap: { if !isBusy { appState.selectCall(id: call.id) } },
                        onLong: { sheet = .menu(call) })
            }
        }
    }

    private var emptyCta: some View {
        Button { sheet = .add("") } label: {
            VStack(spacing: 10) {
                Image(systemName: "plus")
                    .font(.system(size: 22, weight: .regular))
                    .foregroundColor(Palette.accent)
                    .frame(width: 52, height: 52)
                    .background(Circle().fill(Palette.accentSoft))
                Text(NSLocalizedString("empty_calls_title", comment: ""))
                    .font(.system(size: 15, weight: .bold))
                    .foregroundColor(Palette.ink)
                Text(NSLocalizedString("empty_calls_sub", comment: ""))
                    .font(.system(size: 12))
                    .foregroundColor(Palette.ink3)
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 28)
            .background(
                RoundedRectangle(cornerRadius: 18, style: .continuous)
                    .fill(Palette.panel2)
            )
            .overlay(
                RoundedRectangle(cornerRadius: 18, style: .continuous)
                    .stroke(Palette.hairStrong, style: StrokeStyle(lineWidth: 1, dash: [5, 4]))
            )
        }
        .buttonStyle(PlainButtonStyle())
    }

    private var statsCard: some View {
        HStack(spacing: 0) {
            statCell(label: NSLocalizedString("stat_uptime", comment: ""), value: vpn.uptimeText)
            Rectangle().fill(Palette.hair).frame(width: 1, height: 44)
            statCell(label: NSLocalizedString("stat_mode", comment: ""), value: modeLabel)
        }
        .statCard()
    }

    private func statCell(label: String, value: String) -> some View {
        VStack(spacing: 4) {
            Text(label.uppercased())
                .font(Mono.label(9))
                .letterSpacing(1.6)
                .foregroundColor(Palette.ink3)
            Text(value)
                .font(Mono.bold(18))
                .foregroundColor(Palette.ink)
        }
        .frame(maxWidth: .infinity)
        .padding(14)
    }

    private var pingRow: some View {
        VStack(spacing: 10) {
            Button(action: runPing) {
                HStack(spacing: 8) {
                    Image(systemName: "dot.radiowaves.left.and.right").font(.system(size: 14))
                    Text(pinging ? NSLocalizedString("ping_running", comment: "") : NSLocalizedString("ping_run", comment: ""))
                        .font(.system(size: 13, weight: .semibold))
                }
                .foregroundColor(Palette.ink)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 11)
                .overlay(
                    RoundedRectangle(cornerRadius: 12, style: .continuous)
                        .stroke(Palette.hairStrong, lineWidth: 1)
                )
                .opacity(pinging ? 0.6 : 1)
            }
            .buttonStyle(PlainButtonStyle())
            .disabled(pinging)

            if let pingResult { pingResultView(pingResult) }
        }
    }

    @ViewBuilder
    private func pingResultView(_ result: PingResult) -> some View {
        switch result {
        case .ok(let ms):
            pingPill(text: String(format: NSLocalizedString("ping_ok", comment: ""), Ping.host),
                     detail: String(format: NSLocalizedString("ping_ms", comment: ""), ms),
                     color: Palette.accent, background: Palette.accentSoft)
        case .timeout:
            pingPill(text: String(format: NSLocalizedString("ping_fail", comment: ""), Ping.host),
                     detail: NSLocalizedString("ping_timeout", comment: ""),
                     color: Palette.error, background: Palette.error.opacity(0.12))
        }
    }

    private func pingPill(text: String, detail: String, color: Color, background: Color) -> some View {
        HStack(spacing: 8) {
            Text(text).font(Mono.label(11)).foregroundColor(Palette.ink2)
            Spacer(minLength: 8)
            Text(detail).font(Mono.bold(12)).foregroundColor(color)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .background(
            RoundedRectangle(cornerRadius: 12, style: .continuous).fill(background)
        )
    }

    private func runPing() {
        guard !pinging else { return }
        pinging = true
        pingResult = nil
        Task {
            let result = await Ping.run()
            await MainActor.run {
                pingResult = result
                pinging = false
            }
        }
    }

    private func onHero() {
        if isBusy {
            vpn.disconnect()
        } else if appState.activeCall != nil {
            Task { await vpn.connect() }
        }
    }

    @ViewBuilder
    private func sheetView(_ sheet: MainSheet) -> some View {
        switch sheet {
        case .add(let url):
            AddDestinationSheet(appState: appState, prefillUrl: url)
        case .menu(let call):
            MenuSheet(title: call.name, sub: call.url, actions: menuActions(call)) { id in
                handleMenu(id, call: call)
            }
        case .tunnelMode(let call):
            ChoiceSheet(title: NSLocalizedString("settings_row_tunnel_mode", comment: ""),
                        options: tunnelOptions(call.platform),
                        selectedId: (call.tunnelMode ?? appState.tunnelMode).rawValue) { rawValue in
                var updated = call
                updated.tunnelMode = TunnelMode(rawValue: rawValue)
                appState.updateCall(updated)
            }
        case .vp8(let call):
            Vp8Sheet(fps: call.vp8Fps ?? appState.vp8Fps,
                     batch: call.vp8Batch ?? appState.vp8Batch,
                     dualTrack: call.dualTrack ?? appState.dualTrack,
                     reliable: call.reliable ?? appState.reliable) { fps, batch, dual, reliable in
                var updated = call
                updated.vp8Fps = fps
                updated.vp8Batch = batch
                updated.dualTrack = dual
                updated.reliable = reliable
                appState.updateCall(updated)
            }
        case .rename(let call):
            InputSheet(title: NSLocalizedString("destination_rename_title", comment: ""),
                       fieldLabel: NSLocalizedString("sheet_field_name", comment: ""),
                       initial: call.name) { newName in
                appState.renameCall(id: call.id, name: newName)
            }
        case .delete(let call):
            ConfirmSheet(title: NSLocalizedString("destination_delete_title", comment: ""),
                         sub: String(format: NSLocalizedString("destination_delete_confirm", comment: ""), call.name),
                         confirmTitle: NSLocalizedString("confirm_delete", comment: "")) {
                appState.removeCall(id: call.id)
            }
        }
    }

    private func menuActions(_ call: CallConfig) -> [MenuAction] {
        let modeLabel = (call.tunnelMode ?? appState.tunnelMode).label
        let fps = call.vp8Fps ?? appState.vp8Fps
        let batch = call.vp8Batch ?? appState.vp8Batch
        var vp8Sub = String(format: NSLocalizedString("settings_row_vp8_sub", comment: ""), fps, batch)
        if call.dualTrack ?? appState.dualTrack { vp8Sub += " / " + NSLocalizedString("settings_row_vp8_flag_dual", comment: "") }
        if call.reliable ?? appState.reliable { vp8Sub += " / " + NSLocalizedString("settings_row_vp8_flag_kcp", comment: "") }
        return [
            MenuAction(id: "tunnel", title: NSLocalizedString("settings_row_tunnel_mode", comment: ""), icon: "arrow.triangle.branch", value: modeLabel),
            MenuAction(id: "vp8", title: NSLocalizedString("settings_row_vp8", comment: ""), icon: "square.stack.3d.up", value: vp8Sub),
            MenuAction(id: "rename", title: NSLocalizedString("destination_menu_rename", comment: ""), icon: "pencil"),
            MenuAction(id: "delete", title: NSLocalizedString("destination_menu_delete", comment: ""), icon: "trash", danger: true),
        ]
    }

    private func handleMenu(_ id: String, call: CallConfig) {
        switch id {
        case "tunnel": pending = .tunnelMode(call)
        case "vp8": pending = .vp8(call)
        case "rename": pending = .rename(call)
        case "delete": pending = .delete(call)
        default: break
        }
    }

    private func tunnelOptions(_ platform: CallPlatform) -> [ChoiceOption] {
        let modes: [TunnelMode] = (platform == .telemost || platform == .dion) ? [.video] : [.dc, .video]
        return modes.map { ChoiceOption(id: $0.rawValue, title: $0.label) }
    }

    private var modeLabel: String {
        guard let call = appState.activeCall else { return "-" }
        return (call.tunnelMode ?? appState.tunnelMode).label
    }

    private var headerSub: String {
        if isBusy { return NSLocalizedString("main_sub_live", comment: "") }
        let count = appState.savedCalls.count
        if count == 0 { return NSLocalizedString("main_sub_no_configs", comment: "") }
        if count == 1 { return NSLocalizedString("main_sub_count_one", comment: "") }
        return String(format: NSLocalizedString("main_sub_count_other", comment: ""), count)
    }

    private var headline: String {
        switch vpn.phase {
        case .connected: return NSLocalizedString("status_connected", comment: "")
        case .connecting: return NSLocalizedString("status_connecting", comment: "")
        case .disconnected: return NSLocalizedString("status_disconnected", comment: "")
        case .failed: return NSLocalizedString("status_disconnected", comment: "")
        }
    }

    private var defaultDetail: String {
        switch vpn.phase {
        case .connected: return NSLocalizedString("detail_tunnel_active", comment: "")
        case .connecting: return NSLocalizedString("detail_establishing", comment: "")
        case .failed: return vpn.lastError ?? NSLocalizedString("detail_call_failed", comment: "")
        case .disconnected:
            return appState.savedCalls.isEmpty
                ? NSLocalizedString("status_detail_no_calls", comment: "")
                : NSLocalizedString("status_detail_pick_call", comment: "")
        }
    }

    private var dotColor: Color {
        switch vpn.phase {
        case .connected: return Palette.accent
        case .connecting: return Palette.warn
        case .failed: return Palette.error
        case .disconnected: return Palette.ink3
        }
    }
}

