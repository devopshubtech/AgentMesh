import AVFoundation
import CoreImage
import SwiftUI
import UIKit

/// Reads a QR code from a picture (e.g. a screenshot of the dashboard's
/// "Connect a phone" page chosen from Photos).
enum QRImageReader {
    static func read(_ data: Data) -> String? {
        guard let image = UIImage(data: data), let ci = CIImage(image: image) else { return nil }
        let detector = CIDetector(ofType: CIDetectorTypeQRCode, context: nil,
                                  options: [CIDetectorAccuracy: CIDetectorAccuracyHigh])
        let features = detector?.features(in: ci) ?? []
        return features.compactMap { ($0 as? CIQRCodeFeature)?.messageString }.first
    }
}

/// Full-screen camera that reports the first QR code it sees.
struct QRScannerView: UIViewControllerRepresentable {
    var onCode: (String) -> Void
    var onError: (String) -> Void

    func makeUIViewController(context: Context) -> ScannerController {
        let c = ScannerController()
        c.onCode = onCode
        c.onError = onError
        return c
    }

    func updateUIViewController(_ controller: ScannerController, context: Context) {}

    final class ScannerController: UIViewController, AVCaptureMetadataOutputObjectsDelegate {
        var onCode: ((String) -> Void)?
        var onError: ((String) -> Void)?
        private let session = AVCaptureSession()
        private var preview: AVCaptureVideoPreviewLayer?
        private var done = false

        override func viewDidLoad() {
            super.viewDidLoad()
            view.backgroundColor = .black
            AVCaptureDevice.requestAccess(for: .video) { granted in
                DispatchQueue.main.async {
                    granted ? self.setUp() : self.onError?("Allow camera access in Settings → AgentMesh to scan QR codes, or choose the QR code from Photos.")
                }
            }
        }

        private func setUp() {
            guard let device = AVCaptureDevice.default(for: .video),
                  let input = try? AVCaptureDeviceInput(device: device), session.canAddInput(input) else {
                onError?("The camera is not available. Choose the QR code from Photos instead.")
                return
            }
            session.addInput(input)
            let output = AVCaptureMetadataOutput()
            guard session.canAddOutput(output) else { return }
            session.addOutput(output)
            output.setMetadataObjectsDelegate(self, queue: .main)
            output.metadataObjectTypes = [.qr]
            let layer = AVCaptureVideoPreviewLayer(session: session)
            layer.videoGravity = .resizeAspectFill
            layer.frame = view.bounds
            view.layer.addSublayer(layer)
            preview = layer
            DispatchQueue.global(qos: .userInitiated).async { self.session.startRunning() }
        }

        override func viewDidLayoutSubviews() {
            super.viewDidLayoutSubviews()
            preview?.frame = view.bounds
        }

        override func viewWillDisappear(_ animated: Bool) {
            super.viewWillDisappear(animated)
            if session.isRunning { session.stopRunning() }
        }

        func metadataOutput(_ output: AVCaptureMetadataOutput, didOutput objects: [AVMetadataObject], from connection: AVCaptureConnection) {
            guard !done, let code = (objects.first as? AVMetadataMachineReadableCodeObject)?.stringValue else { return }
            done = true
            session.stopRunning()
            onCode?(code)
        }
    }
}
