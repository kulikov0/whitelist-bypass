import Foundation
import Network

enum PingResult {
    case ok(Int)
    case timeout
}

private final class PingOnce: @unchecked Sendable {
    private let lock = NSLock()
    private var done = false
    private let connection: NWConnection
    private let continuation: CheckedContinuation<PingResult, Never>

    init(_ connection: NWConnection, _ continuation: CheckedContinuation<PingResult, Never>) {
        self.connection = connection
        self.continuation = continuation
    }

    func finish(_ result: PingResult) {
        lock.lock()
        if done {
            lock.unlock()
            return
        }
        done = true
        lock.unlock()
        connection.cancel()
        continuation.resume(returning: result)
    }
}

enum Ping {
    static let host = "ya.ru"
    static let port: UInt16 = 443
    static let timeoutSeconds = 5.0

    static func run() async -> PingResult {
        await withCheckedContinuation { continuation in
            let connection = NWConnection(host: NWEndpoint.Host(host), port: NWEndpoint.Port(rawValue: port)!, using: .tls)
            let queue = DispatchQueue(label: "ping.connection")
            let start = DispatchTime.now()
            let once = PingOnce(connection, continuation)

            connection.stateUpdateHandler = { state in
                switch state {
                case .ready:
                    let elapsedNs = DispatchTime.now().uptimeNanoseconds - start.uptimeNanoseconds
                    once.finish(.ok(Int(Double(elapsedNs) / 1_000_000)))
                case .failed, .cancelled:
                    once.finish(.timeout)
                default:
                    break
                }
            }

            queue.asyncAfter(deadline: .now() + timeoutSeconds) { once.finish(.timeout) }
            connection.start(queue: queue)
        }
    }
}
