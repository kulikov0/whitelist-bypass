import SwiftUI
import CoreImage.CIFilterBuiltins

struct BlinkLoginScreen: View {
    let link: String
    let onClose: () -> Void

    @State private var copied = false

    private static let qrContext = CIContext()
    private static let qrSize: CGFloat = 204

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 10) {
                Image(systemName: "person.badge.key")
                    .font(.system(size: 14))
                    .foregroundStyle(Palette.accent)
                Text(NSLocalizedString("blink_login_title", comment: ""))
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

            ScrollView {
                VStack(spacing: 16) {
                    Text(NSLocalizedString("blink_login_subtitle", comment: ""))
                        .font(.system(size: 13))
                        .foregroundStyle(Palette.ink2)
                        .multilineTextAlignment(.center)

                    if let qr = qrImage(link) {
                        Image(uiImage: qr)
                            .interpolation(.none)
                            .resizable()
                            .frame(width: Self.qrSize, height: Self.qrSize)
                            .padding(8)
                            .background(
                                RoundedRectangle(cornerRadius: 12, style: .continuous)
                                    .fill(Color.white)
                            )
                    }

                    Text(link)
                        .font(Mono.label(11))
                        .foregroundStyle(Palette.ink2)
                        .textSelection(.enabled)

                    HStack(spacing: 8) {
                        Button {
                            UIPasteboard.general.string = link
                            copied = true
                        } label: {
                            loginButton(icon: "doc.on.doc", title: NSLocalizedString(copied ? "blink_login_copied" : "blink_login_copy", comment: ""))
                        }
                        .buttonStyle(.plain)

                        Button {
                            if let url = URL(string: link) {
                                UIApplication.shared.open(url)
                            }
                        } label: {
                            loginButton(icon: "arrow.up.forward.app", title: NSLocalizedString("blink_login_open", comment: ""))
                        }
                        .buttonStyle(.plain)
                    }
                }
                .padding(20)
            }
        }
        .background(Palette.surface.ignoresSafeArea())
        .onChange(of: link) { _ in copied = false }
    }

    private func loginButton(icon: String, title: String) -> some View {
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

    private func qrImage(_ text: String) -> UIImage? {
        let filter = CIFilter.qrCodeGenerator()
        filter.message = Data(text.utf8)
        guard let output = filter.outputImage,
              let cgImage = Self.qrContext.createCGImage(output, from: output.extent) else { return nil }
        return UIImage(cgImage: cgImage)
    }
}
