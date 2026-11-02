import Foundation

enum PingResult {
    case ok(Int)
    case timeout
}

enum Ping {
    static let host = "google.com"
    static let port: UInt16 = 443
    static let readTimeoutSeconds = 15

    static func run(socksPort: Int, user: String, pass: String) -> PingResult {
        let start = DispatchTime.now()
        guard probeViaSocks5(socksPort: socksPort, user: user, pass: pass) else { return .timeout }
        let elapsedNs = DispatchTime.now().uptimeNanoseconds - start.uptimeNanoseconds
        return .ok(Int(Double(elapsedNs) / 1_000_000))
    }

    private static func probeViaSocks5(socksPort: Int, user: String, pass: String) -> Bool {
        let userBytes = Array(user.utf8)
        let passBytes = Array(pass.utf8)
        let hostBytes = Array(host.utf8)
        guard userBytes.count <= UInt8.max, passBytes.count <= UInt8.max else { return false }

        let socketFD = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP)
        if socketFD == -1 { return false }
        defer { close(socketFD) }

        var noSigPipe: Int32 = 1
        setsockopt(socketFD, SOL_SOCKET, SO_NOSIGPIPE, &noSigPipe, socklen_t(MemoryLayout<Int32>.size))
        var readTimeout = timeval(tv_sec: readTimeoutSeconds, tv_usec: 0)
        setsockopt(socketFD, SOL_SOCKET, SO_RCVTIMEO, &readTimeout, socklen_t(MemoryLayout<timeval>.size))

        var addr = sockaddr_in()
        addr.sin_len = UInt8(MemoryLayout<sockaddr_in>.size)
        addr.sin_family = sa_family_t(AF_INET)
        addr.sin_port = in_port_t(socksPort).bigEndian
        addr.sin_addr.s_addr = in_addr_t(INADDR_LOOPBACK).bigEndian
        let connected = withUnsafePointer(to: &addr) { addrPtr in
            addrPtr.withMemoryRebound(to: sockaddr.self, capacity: 1) { sockaddrPtr in
                connect(socketFD, sockaddrPtr, socklen_t(MemoryLayout<sockaddr_in>.size))
            }
        }
        if connected != 0 { return false }

        guard sendAll(socketFD, [0x05, 0x01, 0x02]), readBytes(socketFD, 2) == [0x05, 0x02] else { return false }

        let authPacket = [0x01, UInt8(userBytes.count)] + userBytes + [UInt8(passBytes.count)] + passBytes
        guard sendAll(socketFD, authPacket), readBytes(socketFD, 2) == [0x01, 0x00] else { return false }

        let request = [0x05, 0x01, 0x00, 0x03, UInt8(hostBytes.count)] + hostBytes + [UInt8(port >> 8), UInt8(port & 0xff)]
        guard sendAll(socketFD, request) else { return false }
        return readBytes(socketFD, 2) == [0x05, 0x00]
    }

    private static func sendAll(_ socketFD: Int32, _ bytes: [UInt8]) -> Bool {
        var offset = 0
        while offset < bytes.count {
            let sent = bytes[offset...].withUnsafeBytes { buffer in
                send(socketFD, buffer.baseAddress, buffer.count, 0)
            }
            if sent <= 0 { return false }
            offset += sent
        }
        return true
    }

    private static func readBytes(_ socketFD: Int32, _ count: Int) -> [UInt8]? {
        var buffer = [UInt8](repeating: 0, count: count)
        var offset = 0
        while offset < count {
            let received = buffer[offset...].withUnsafeMutableBytes { slice in
                recv(socketFD, slice.baseAddress, slice.count, 0)
            }
            if received <= 0 { return nil }
            offset += received
        }
        return buffer
    }
}
