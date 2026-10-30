import SwiftUI
import CoreImage.CIFilterBuiltins

struct BlinkLoginView: View {
    let link: String
    let onCopy: () -> Void
    let onOpen: () -> Void

    private static let qrContext = CIContext()
    private static let qrSize: CGFloat = 204

    var body: some View {
        VStack(spacing: 12) {
            Text(NSLocalizedString("blink_login_title", comment: ""))
                .font(.headline)

            Text(NSLocalizedString("blink_login_subtitle", comment: ""))
                .font(.caption)
                .foregroundColor(.secondary)
                .multilineTextAlignment(.center)

            if let qr = qrImage(link) {
                Image(uiImage: qr)
                    .interpolation(.none)
                    .resizable()
                    .frame(width: Self.qrSize, height: Self.qrSize)
                    .padding(8)
                    .background(Color.white)
                    .cornerRadius(8)
            }

            Text(link)
                .font(.system(.caption, design: .monospaced))
                .textSelection(.enabled)

            HStack {
                Button(action: onCopy) {
                    Label(NSLocalizedString("blink_login_copy", comment: ""), systemImage: "doc.on.doc")
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(.bordered)

                Button(action: onOpen) {
                    Label(NSLocalizedString("blink_login_open", comment: ""), systemImage: "arrow.up.forward.app")
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
            }
        }
        .padding(.horizontal)
    }

    private func qrImage(_ text: String) -> UIImage? {
        let filter = CIFilter.qrCodeGenerator()
        filter.message = Data(text.utf8)
        guard let output = filter.outputImage,
              let cgImage = Self.qrContext.createCGImage(output, from: output.extent) else { return nil }
        return UIImage(cgImage: cgImage)
    }
}
