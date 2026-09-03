import SwiftUI
import WebKit

struct CaptchaWebView: UIViewRepresentable {
    let url: URL

    func makeUIView(context: Context) -> WKWebView {
        let webView = WKWebView()
        webView.customUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
        webView.load(URLRequest(url: url))
        return webView
    }

    func updateUIView(_ uiView: WKWebView, context: Context) {}
}

struct CaptchaScreen: View {
    let url: URL
    let onClose: () -> Void

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 10) {
                Image(systemName: "checkmark.shield")
                    .font(.system(size: 14))
                    .foregroundStyle(Palette.accent)
                Text(NSLocalizedString("status_solve_captcha", comment: ""))
                    .font(Mono.label(12))
                    .foregroundStyle(Palette.ink)
                Spacer()
                Button(action: onClose) {
                    Image(systemName: "xmark")
                        .font(.system(size: 15, weight: .semibold))
                        .foregroundStyle(Palette.ink2)
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
                .buttonStyle(.plain)
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 12)

            Rectangle().fill(Palette.hair).frame(height: 1)

            CaptchaWebView(url: url)
                .ignoresSafeArea(edges: .bottom)
        }
        .background(Palette.surface.ignoresSafeArea())
    }
}
