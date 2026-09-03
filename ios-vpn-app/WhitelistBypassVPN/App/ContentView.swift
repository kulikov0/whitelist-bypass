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
    @EnvironmentObject var appState: AppState
    @EnvironmentObject var vpn: VPNController
    @State private var tab: NavTab = .main

    var body: some View {
        ZStack {
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
            .background(Palette.surface.edgesIgnoringSafeArea(.all))

            if let raw = vpn.captchaURL, let url = URL(string: raw) {
                CaptchaScreen(url: url) {
                    vpn.captchaURL = nil
                    vpn.disconnect()
                }
                .transition(.opacity)
            }
        }
        .preferredColorScheme(appState.themeMode.colorScheme)
        .onAppear {
            vpn.appState = appState
            if appState.connectOnStart, appState.activeCall != nil {
                Task { await vpn.connect() }
            }
        }
        .onReceive(NotificationCenter.default.publisher(for: UIApplication.didBecomeActiveNotification)) { _ in
            vpn.startPolling()
        }
        .onReceive(NotificationCenter.default.publisher(for: UIApplication.didEnterBackgroundNotification)) { _ in
            vpn.stopPolling()
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
                                .fontWeight(tab == item ? .bold : .regular)
                                .letterSpacing(1.4)
                        }
                        .foregroundColor(tab == item ? Palette.accent : Palette.ink3)
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 10)
                        .padding(.horizontal, 6)
                        .background(
                            RoundedRectangle(cornerRadius: 14, style: .continuous)
                                .fill(tab == item ? Palette.accentSoft : Color.clear)
                        )
                    }
                    .buttonStyle(PlainButtonStyle())
                }
            }
            .padding(.horizontal, 16)
            .padding(.top, 8)
            .padding(.bottom, 12)
        }
        .background(Palette.surface)
    }
}
