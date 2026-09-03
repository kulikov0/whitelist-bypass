import Foundation
import Mobile

final class HeadlessCallback: NSObject, IosHeadlessCallbackProtocol {
    weak var provider: PacketTunnelProvider?

    func onLog(_ msg: String?) {
        guard let msg else { return }
        print("[GO] \(msg)")
        LogWriter.shared.append(msg)
    }

    func onStatus(_ status: String?) {
        guard let status else { return }
        print("[STATUS] \(status)")
        StatusHolder.shared.set(status)
        if status.hasPrefix("ERROR:") {
            let message = String(status.dropFirst("ERROR:".count))
            DispatchQueue.main.async { [weak self] in
                self?.provider?.failSession(message)
            }
        } else if status == GoStatus.tunnelConnected {
            DispatchQueue.main.async { [weak self] in
                self?.provider?.relayConnected()
            }
        }
    }

    func saveCache(_ key: String?, value: String?) {
        guard let key, let value else { return }
        TokenCache.save(key, value)
    }

    func loadCache(_ key: String?) -> String {
        guard let key else { return "" }
        return TokenCache.load(key)
    }

    func clearCache(_ key: String?) {
        guard let key else { return }
        TokenCache.clear(key)
    }

    func resolveHost(_ hostname: String?) -> String {
        guard let hostname else { return "" }
        Log.relay.debug("resolveHost \(hostname, privacy: .public)")
        let host = CFHostCreateWithName(nil, hostname as CFString).takeRetainedValue()
        CFHostStartInfoResolution(host, .addresses, nil)
        guard let addresses = CFHostGetAddressing(host, nil)?.takeUnretainedValue() as? [Data] else {
            Log.relay.error("resolveHost \(hostname, privacy: .public) FAILED")
            return ""
        }
        for addressData in addresses {
            var storage = sockaddr_storage()
            addressData.withUnsafeBytes { buffer in
                if let base = buffer.baseAddress {
                    memcpy(&storage, base, min(addressData.count, MemoryLayout<sockaddr_storage>.size))
                }
            }
            if storage.ss_family == UInt8(AF_INET) {
                var addr = sockaddr_in()
                addressData.withUnsafeBytes { buffer in
                    if let base = buffer.baseAddress {
                        memcpy(&addr, base, MemoryLayout<sockaddr_in>.size)
                    }
                }
                var ipString = [CChar](repeating: 0, count: Int(INET_ADDRSTRLEN))
                var inAddr = addr.sin_addr
                inet_ntop(AF_INET, &inAddr, &ipString, socklen_t(INET_ADDRSTRLEN))
                return String(cString: ipString)
            }
        }
        return ""
    }
}
