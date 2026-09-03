import SwiftUI

@main
struct WhitelistBypassVPNApp: App {
    @StateObject private var appState = AppState()
    @StateObject private var vpn = VPNController()
    @Environment(\.scenePhase) private var scenePhase

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(appState)
                .environmentObject(vpn)
                .preferredColorScheme(appState.themeMode.colorScheme)
                .onAppear {
                    vpn.appState = appState
                    if appState.connectOnStart, appState.activeCall != nil {
                        Task { await vpn.connect() }
                    }
                }
                .onChange(of: scenePhase) { phase in
                    switch phase {
                    case .active: vpn.startPolling()
                    case .background: vpn.stopPolling()
                    default: break
                    }
                }
        }
    }
}
