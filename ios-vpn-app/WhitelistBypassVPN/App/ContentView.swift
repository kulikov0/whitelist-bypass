import SwiftUI

enum NavTab: CaseIterable {
    case main
    case settings
    case logs

    var title: String {
        switch self {
        case .main: return NSLocalizedString("nav_main", comment: "")
        case .settings: return NSLocalizedString("nav_settings", comment: "")
        case .logs: return NSLocalizedString("nav_logs", comment: "")
        }
    }

    var icon: String {
        switch self {
        case .main: return "shield.lefthalf.filled"
        case .settings: return "gearshape"
        case .logs: return "list.bullet.rectangle"
        }
    }
}

struct ContentView: View {
    @EnvironmentObject var vpn: VPNController
    @State private var tab: NavTab = .main

    var body: some View {
        VStack(spacing: 0) {
            ZStack {
                switch tab {
                case .main: MainScreen()
                case .settings: SettingsScreen()
                case .logs: LogsScreen()
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)

            BottomNav(tab: $tab)
        }
        .background(Palette.surface.ignoresSafeArea())
        .fullScreenCover(isPresented: Binding(
            get: { vpn.captchaURL != nil },
            set: { if !$0 { vpn.captchaURL = nil } }
        )) {
            if let raw = vpn.captchaURL, let url = URL(string: raw) {
                CaptchaScreen(url: url) { vpn.disconnect() }
            }
        }
    }
}

struct BottomNav: View {
    @Binding var tab: NavTab

    var body: some View {
        VStack(spacing: 0) {
            Rectangle()
                .fill(Palette.hair)
                .frame(height: 1)

            HStack(spacing: 8) {
                ForEach(NavTab.allCases, id: \.self) { item in
                    Button {
                        tab = item
                    } label: {
                        VStack(spacing: 4) {
                            Image(systemName: item.icon)
                                .font(.system(size: 18, weight: .regular))
                                .frame(width: 22, height: 22)
                            Text(item.title.uppercased())
                                .font(Mono.label(10))
                                .tracking(1.4)
                                .fontWeight(tab == item ? .bold : .regular)
                        }
                        .foregroundStyle(tab == item ? Palette.accent : Palette.ink3)
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 10)
                        .padding(.horizontal, 6)
                        .background(
                            RoundedRectangle(cornerRadius: 14, style: .continuous)
                                .fill(tab == item ? Palette.accentSoft : Color.clear)
                        )
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(.horizontal, 16)
            .padding(.top, 8)
            .padding(.bottom, 12)
        }
        .background(Palette.surface)
    }
}
