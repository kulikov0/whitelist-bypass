import SwiftUI

@main
struct WhitelistBypassProxyApp: App {
    @StateObject private var proxyManager = ProxyManager()

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(proxyManager)
        }
    }
}
