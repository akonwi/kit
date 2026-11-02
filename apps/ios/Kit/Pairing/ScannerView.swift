import AVFoundation
import KitClient
import SwiftUI

/// The full-screen camera that reads the QR code `kit server pair` shows.
struct ScannerView: View {
    @Environment(PairingCoordinator.self) private var pairing
    @Environment(\.openURL) private var openURL
    @State private var access = AVCaptureDevice.authorizationStatus(for: .video)
    @State private var hasCamera = AVCaptureDevice.default(for: .video) != nil
    @State private var foreignCode = false
    @State private var found = false

    private static let viewfinder: CGFloat = 250

    var body: some View {
        ZStack {
            LinearGradient(colors: [Color(white: 0.24), Color(white: 0.11), Color(red: 0.17, green: 0.15, blue: 0.13)],
                           startPoint: .topLeading, endPoint: .bottomTrailing)
            if access == .authorized && hasCamera {
                QRCameraView(onCode: read)
            }
            Color.black.opacity(0.45)
                .mask {
                    Rectangle()
                        .overlay {
                            RoundedRectangle(cornerRadius: 28)
                                .frame(width: Self.viewfinder, height: Self.viewfinder)
                                .blendMode(.destinationOut)
                        }
                        .compositingGroup()
                }
            VStack(spacing: 34) {
                RoundedRectangle(cornerRadius: 28)
                    .strokeBorder(.white.opacity(0.92), lineWidth: 3)
                    .frame(width: Self.viewfinder, height: Self.viewfinder)
                    .overlay { cameraNote }
                    // The viewfinder sits on the screen's center line; the
                    // captions hang below it.
                    .padding(.top, 110)
                captions
                    .frame(height: 110, alignment: .top)
            }
        }
        .ignoresSafeArea()
        .overlay(alignment: .topLeading) {
            Button { pairing.scanning = false } label: {
                Image(systemName: "xmark")
                    .font(.system(size: 16, weight: .semibold))
                    .frame(width: 44, height: 44)
                    .contentShape(Circle())
            }
            .buttonStyle(.plain)
            .glassEffect(.regular.interactive(), in: Circle())
            .accessibilityLabel("Close")
            .padding(.leading, 16)
        }
        .overlay(alignment: .bottom) {
            Button { pairing.pasteInstead() } label: {
                Text("Paste link instead")
                    .font(.system(size: 17, weight: .medium))
                    .frame(width: 210, height: 50)
                    .contentShape(Capsule())
            }
            .buttonStyle(.plain)
            .glassEffect(.regular.interactive(), in: Capsule())
            .padding(.bottom, 22)
        }
        .foregroundStyle(.white)
        .environment(\.colorScheme, .dark)
        .sensoryFeedback(.success, trigger: found)
        .task {
            guard hasCamera, access == .notDetermined else { return }
            _ = await AVCaptureDevice.requestAccess(for: .video)
            access = AVCaptureDevice.authorizationStatus(for: .video)
        }
    }

    private var captions: some View {
        VStack(spacing: 6) {
            Text("Scan the code from kit server pair")
                .font(.system(size: 17, weight: .semibold))
            Text(foreignCode ? "That code isn’t a Kit pairing link." : "Codes work once and expire after 5 minutes.")
                .font(.system(size: 15))
                .foregroundStyle(.white.opacity(0.7))
        }
        .multilineTextAlignment(.center)
        .padding(.horizontal, 40)
    }

    @ViewBuilder
    private var cameraNote: some View {
        if !hasCamera {
            Text("This device has no camera. Paste the link instead.")
                .font(.system(size: 15))
                .foregroundStyle(.white.opacity(0.7))
                .multilineTextAlignment(.center)
                .padding(24)
        } else if access == .denied || access == .restricted {
            VStack(spacing: 12) {
                Text("Allow camera access to scan.")
                    .font(.system(size: 15))
                    .foregroundStyle(.white.opacity(0.7))
                    .multilineTextAlignment(.center)
                Button("Open Settings") {
                    if let url = URL(string: UIApplication.openSettingsURLString) { openURL(url) }
                }
                .buttonStyle(.glass)
            }
            .padding(24)
        }
    }

    private func read(_ value: String) {
        guard !found else { return }
        guard let link = PairingLink(value) else {
            foreignCode = true
            return
        }
        found = true
        pairing.scanned(link)
    }
}

/// The camera preview, reporting each QR code it reads.
private struct QRCameraView: UIViewRepresentable {
    let onCode: @MainActor (String) -> Void

    func makeCoordinator() -> QRScanner { QRScanner(onCode: onCode) }

    func makeUIView(context: Context) -> PreviewView {
        let view = PreviewView()
        view.previewLayer.session = context.coordinator.session
        view.previewLayer.videoGravity = .resizeAspectFill
        context.coordinator.start()
        return view
    }

    func updateUIView(_ view: PreviewView, context: Context) {}

    static func dismantleUIView(_ view: PreviewView, coordinator: QRScanner) {
        coordinator.stop()
    }

    final class PreviewView: UIView {
        override class var layerClass: AnyClass { AVCaptureVideoPreviewLayer.self }
        var previewLayer: AVCaptureVideoPreviewLayer { layer as! AVCaptureVideoPreviewLayer }
    }
}

/// Runs a capture session for QR codes on its own queue. The session is
/// touched only on that queue.
private final class QRScanner: NSObject, AVCaptureMetadataOutputObjectsDelegate, @unchecked Sendable {
    let session = AVCaptureSession()
    private let queue = DispatchQueue(label: "com.akonwi.kit.qr-scanner")
    private let onCode: @MainActor (String) -> Void

    init(onCode: @escaping @MainActor (String) -> Void) {
        self.onCode = onCode
    }

    func start() {
        queue.async { [self] in
            guard session.inputs.isEmpty else { return session.startRunning() }
            guard let camera = AVCaptureDevice.default(for: .video),
                  let input = try? AVCaptureDeviceInput(device: camera), session.canAddInput(input) else { return }
            let output = AVCaptureMetadataOutput()
            session.beginConfiguration()
            session.addInput(input)
            guard session.canAddOutput(output) else { return session.commitConfiguration() }
            session.addOutput(output)
            output.setMetadataObjectsDelegate(self, queue: queue)
            output.metadataObjectTypes = [.qr]
            session.commitConfiguration()
            session.startRunning()
        }
    }

    func stop() {
        queue.async { [self] in session.stopRunning() }
    }

    func metadataOutput(_ output: AVCaptureMetadataOutput, didOutput objects: [AVMetadataObject], from connection: AVCaptureConnection) {
        guard let value = objects.compactMap({ ($0 as? AVMetadataMachineReadableCodeObject)?.stringValue }).first else { return }
        let onCode = onCode
        Task { @MainActor in onCode(value) }
    }
}
