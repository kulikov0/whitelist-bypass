import Foundation

final class LogWriter {
    static let shared = LogWriter()

    private let lock = NSLock()
    private var displayLines: [String] = []
    private var revisionCounter = 0
    private let maxDisplayLines = 2000
    private let fileURL = AppIdentifiers.relayLogURL
    private var handle: FileHandle?
    private let formatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "HH:mm:ss.SSS"
        f.locale = Locale(identifier: "en_US_POSIX")
        return f
    }()

    func reset() {
        lock.lock()
        defer { lock.unlock() }
        displayLines.removeAll()
        revisionCounter += 1
        handle?.closeFile()
        handle = nil
        if let fileURL {
            FileManager.default.createFile(atPath: fileURL.path, contents: nil)
            handle = try? FileHandle(forWritingTo: fileURL)
        }
    }

    func append(_ msg: String) {
        let line = formatter.string(from: Date()) + " " + msg
        Log.relay.log("\(line, privacy: .public)")
        lock.lock()
        defer { lock.unlock() }
        displayLines.append(line)
        if displayLines.count > maxDisplayLines {
            displayLines.removeFirst(displayLines.count - maxDisplayLines)
        }
        revisionCounter += 1
        if let data = (line + "\n").data(using: .utf8) { handle?.write(data) }
    }

    func revision() -> Int {
        lock.lock()
        defer { lock.unlock() }
        return revisionCounter
    }

    func displayText() -> [String] {
        lock.lock()
        defer { lock.unlock() }
        return displayLines
    }
}
