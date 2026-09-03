import Foundation

final class StatusHolder {
    static let shared = StatusHolder()

    private let lock = NSLock()
    private var value = GoStatus.idle

    func set(_ newValue: String) {
        Log.status.log("status = \(newValue, privacy: .public)")
        lock.lock()
        value = newValue
        lock.unlock()
    }

    func get() -> String {
        lock.lock()
        defer { lock.unlock() }
        return value
    }
}
