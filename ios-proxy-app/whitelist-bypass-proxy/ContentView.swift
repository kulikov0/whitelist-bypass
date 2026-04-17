import SwiftUI

struct ContentView: View {
    @EnvironmentObject var proxyManager: ProxyManager
    @State private var showSettings = false

    var body: some View {
        NavigationView {
            VStack(spacing: 0) {
                VStack(spacing: 12) {
                    HStack {
                        ZStack(alignment: .trailing) {
                            TextField("VK or Telemost call link", text: $proxyManager.callUrl)
                                .textFieldStyle(.roundedBorder)
                                .autocapitalization(.none)
                                .disableAutocorrection(true)
                                .keyboardType(.URL)
                                .padding(.trailing, proxyManager.callUrl.isEmpty ? 0 : 24)

                            if !proxyManager.callUrl.isEmpty {
                                Button(action: { proxyManager.callUrl = "" }) {
                                    Image(systemName: "xmark.circle.fill")
                                        .foregroundColor(.gray)
                                }
                                .padding(.trailing, 6)
                            }
                        }

                        Button(action: {
                            if proxyManager.isRunning {
                                proxyManager.resetAll()
                            } else {
                                proxyManager.connect()
                            }
                        }) {
                            Text(proxyManager.isRunning ? "Stop" : "Go")
                                .fontWeight(.bold)
                                .frame(width: 60)
                        }
                        .buttonStyle(.borderedProminent)
                        .tint(proxyManager.isRunning ? .red : .green)
                    }
                    .padding(.horizontal)

                    if let captchaURL = proxyManager.captchaURL, let url = URL(string: captchaURL) {
                        CaptchaWebView(url: url)
                            .frame(maxWidth: .infinity, maxHeight: .infinity)
                    }

                    if proxyManager.status == .tunnelConnected {
                        ProxyInfoView(proxyUrl: proxyManager.socksUrl, onCopy: proxyManager.copyProxyUrl)

                        Button(action: { proxyManager.openTelegramProxy() }) {
                            Label("Open in Telegram", systemImage: "paperplane.fill")
                                .frame(maxWidth: .infinity)
                        }
                        .buttonStyle(.borderedProminent)
                        .tint(.blue)
                        .padding(.horizontal)
                    }
                }
                .padding(.vertical, 12)

                if proxyManager.showLogs && proxyManager.captchaURL == nil {
                    LogView(logs: proxyManager.logs)
                }

                Spacer(minLength: 0)
            }
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .navigationBarLeading) {
                    StatusIndicator(status: proxyManager.status, errorMessage: proxyManager.errorMessage, statusText: proxyManager.statusText)
                }
                ToolbarItem(placement: .navigationBarTrailing) {
                    Button(action: { showSettings = true }) {
                        Image(systemName: "gearshape")
                    }
                }
            }
            .sheet(isPresented: $showSettings) {
                SettingsView()
                    .environmentObject(proxyManager)
            }
            .onTapGesture {
                UIApplication.shared.sendAction(#selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil)
            }
            .overlay(alignment: .bottom) {
                if let toast = proxyManager.toastMessage {
                    Text(toast)
                        .font(.subheadline)
                        .fontWeight(.medium)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 10)
                        .background(.ultraThinMaterial)
                        .cornerRadius(20)
                        .padding(.bottom, 40)
                        .transition(.move(edge: .bottom).combined(with: .opacity))
                        .animation(.easeInOut(duration: 0.3), value: proxyManager.toastMessage)
                }
            }
        }
    }
}

struct StatusIndicator: View {
    let status: ProxyStatus
    let errorMessage: String
    let statusText: String?

    var statusColor: Color {
        if statusText != nil { return .yellow }
        switch status {
        case .idle: return .gray
        case .ready: return .gray
        case .connecting: return .yellow
        case .tunnelConnected: return .green
        case .tunnelLost: return .orange
        case .error: return .red
        }
    }

    var displayText: String {
        if let text = statusText { return text }
        if !errorMessage.isEmpty { return errorMessage }
        return status.displayLabel
    }

    var body: some View {
        HStack(spacing: 6) {
            Circle()
                .fill(statusColor)
                .frame(width: 8, height: 8)
            Text(displayText)
                .font(.subheadline)
                .fontWeight(.medium)
                .lineLimit(1)
        }
    }
}

struct ProxyInfoView: View {
    let proxyUrl: String
    let onCopy: () -> Void

    var body: some View {
        HStack {
            Text(proxyUrl)
                .font(.system(.caption, design: .monospaced))
                .lineLimit(1)
                .truncationMode(.middle)
            Button(action: onCopy) {
                Image(systemName: "doc.on.doc")
            }
        }
        .padding(.horizontal)
        .padding(.vertical, 6)
        .background(Color.green.opacity(0.1))
        .cornerRadius(8)
        .padding(.horizontal)
    }
}

struct LogView: View {
    let logs: [String]
    @State private var userScrolledUp = false

    var body: some View {
        ScrollViewReader { scrollProxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0) {
                    ForEach(logs.indices, id: \.self) { index in
                        Text(logs[index])
                            .font(.system(.caption2, design: .monospaced))
                            .foregroundColor(.secondary)
                    }
                }
                .padding(.horizontal, 8)
                .padding(.vertical, 4)
            }
            .background(Color(.systemGroupedBackground))
            .simultaneousGesture(DragGesture().onChanged { _ in
                userScrolledUp = true
            })
            .onChange(of: logs.count) { _ in
                if !userScrolledUp, let last = logs.indices.last {
                    scrollProxy.scrollTo(last, anchor: .bottom)
                }
            }
        }
    }
}

struct SettingsView: View {
    @EnvironmentObject var proxyManager: ProxyManager
    @Environment(\.dismiss) var dismiss

    var body: some View {
        NavigationView {
            Form {
                Section("Tunnel") {
                    Picker("Mode", selection: $proxyManager.tunnelMode) {
                        Text("DataChannel").tag("dc")
                        Text("Video (VP8)").tag("video")
                    }
                }

                Section("Proxy") {
                    Picker("Auth Mode", selection: $proxyManager.socksAuthMode) {
                        Text("Auto").tag(SocksAuthMode.auto)
                        Text("Manual").tag(SocksAuthMode.manual)
                    }

                    if proxyManager.socksAuthMode == .manual {
                        TextField("Username", text: $proxyManager.manualSocksUser)
                            .autocapitalization(.none)
                            .disableAutocorrection(true)
                        TextField("Password", text: $proxyManager.manualSocksPass)
                            .autocapitalization(.none)
                            .disableAutocorrection(true)
                    }
                }

                Section("Display") {
                    TextField("Display Name", text: $proxyManager.displayName)
                    Toggle("Show Logs", isOn: $proxyManager.showLogs)
                }

            }
            .navigationTitle("Settings")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .navigationBarTrailing) {
                    Button("Done") { dismiss() }
                }
            }
        }
    }
}
