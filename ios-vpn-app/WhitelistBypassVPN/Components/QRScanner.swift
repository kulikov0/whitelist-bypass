import SwiftUI
import AVFoundation

extension View {
    func dismissKeyboard() {
        UIApplication.shared.sendAction(#selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil)
    }
}

struct QRScannerSheet: View {
    let onResult: (String) -> Void
    @Environment(\.presentationMode) private var presentationMode
    @State private var status = AVCaptureDevice.authorizationStatus(for: .video)

    var body: some View {
        ZStack {
            Color.black.edgesIgnoringSafeArea(.all)

            switch status {
            case .authorized:
                camera
                viewfinder
            case .notDetermined:
                LegacySpinner()
                    .onAppear(perform: requestAccess)
            default:
                denied
            }

            VStack {
                HStack {
                    Spacer()
                    Button { presentationMode.wrappedValue.dismiss() } label: {
                        Image(systemName: "xmark")
                            .font(.system(size: 15, weight: .semibold))
                            .foregroundColor(.white)
                            .frame(width: 40, height: 40)
                            .background(Circle().fill(Color.black.opacity(0.4)))
                    }
                }
                Spacer()
            }
            .padding(20)
        }
    }

    private var camera: some View {
        QRCameraView { value in
            let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !trimmed.isEmpty else { return }
            onResult(trimmed)
            presentationMode.wrappedValue.dismiss()
        }
        .edgesIgnoringSafeArea(.all)
    }

    private var viewfinder: some View {
        VStack(spacing: 20) {
            RoundedRectangle(cornerRadius: 24, style: .continuous)
                .stroke(Palette.accent, lineWidth: 3)
                .frame(width: 240, height: 240)
                .shadow(color: .black.opacity(0.35), radius: 12)
            Text(NSLocalizedString("qr_prompt", comment: ""))
                .font(Mono.label(12))
                .foregroundColor(.white)
        }
    }

    private var denied: some View {
        VStack(spacing: 16) {
            Image(systemName: "camera.metering.none")
                .font(.system(size: 36))
                .foregroundColor(.white.opacity(0.7))
            Text(NSLocalizedString("qr_denied_title", comment: ""))
                .font(.system(size: 15, weight: .semibold))
                .foregroundColor(.white)
            Text(NSLocalizedString("qr_denied_sub", comment: ""))
                .font(.system(size: 13))
                .foregroundColor(.white.opacity(0.6))
                .multilineTextAlignment(.center)
            Button(NSLocalizedString("qr_open_settings", comment: "")) {
                if let url = URL(string: UIApplication.openSettingsURLString) {
                    UIApplication.shared.open(url)
                }
            }
            .font(.system(size: 13, weight: .semibold))
            .foregroundColor(Palette.accent)
        }
        .padding(32)
    }

    private func requestAccess() {
        AVCaptureDevice.requestAccess(for: .video) { granted in
            DispatchQueue.main.async {
                status = granted ? .authorized : .denied
            }
        }
    }
}

struct QRCameraView: UIViewControllerRepresentable {
    let onFound: (String) -> Void

    func makeUIViewController(context: Context) -> QRCameraController {
        let controller = QRCameraController()
        controller.onFound = onFound
        return controller
    }

    func updateUIViewController(_ uiViewController: QRCameraController, context: Context) {}
}

final class QRCameraController: UIViewController, AVCaptureMetadataOutputObjectsDelegate {
    var onFound: ((String) -> Void)?

    private let session = AVCaptureSession()
    private let sessionQueue = DispatchQueue(label: "qr.session")
    private var previewLayer: AVCaptureVideoPreviewLayer?
    private var handled = false

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .black
        configureSession()
    }

    private func configureSession() {
        guard let device = AVCaptureDevice.default(for: .video),
              let input = try? AVCaptureDeviceInput(device: device),
              session.canAddInput(input) else { return }
        session.addInput(input)

        let output = AVCaptureMetadataOutput()
        guard session.canAddOutput(output) else { return }
        session.addOutput(output)
        output.setMetadataObjectsDelegate(self, queue: .main)
        output.metadataObjectTypes = [.qr]

        let layer = AVCaptureVideoPreviewLayer(session: session)
        layer.videoGravity = .resizeAspectFill
        layer.frame = view.layer.bounds
        view.layer.addSublayer(layer)
        previewLayer = layer
    }

    override func viewWillAppear(_ animated: Bool) {
        super.viewWillAppear(animated)
        sessionQueue.async { [weak self] in
            guard let self, !self.session.isRunning else { return }
            self.session.startRunning()
        }
    }

    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        previewLayer?.frame = view.layer.bounds
    }

    override func viewWillDisappear(_ animated: Bool) {
        super.viewWillDisappear(animated)
        sessionQueue.async { [weak self] in
            guard let self, self.session.isRunning else { return }
            self.session.stopRunning()
        }
    }

    func metadataOutput(_ output: AVCaptureMetadataOutput, didOutput metadataObjects: [AVMetadataObject], from connection: AVCaptureConnection) {
        guard !handled,
              let object = metadataObjects.first as? AVMetadataMachineReadableCodeObject,
              let value = object.stringValue else { return }
        handled = true
        UINotificationFeedbackGenerator().notificationOccurred(.success)
        onFound?(value)
    }
}
