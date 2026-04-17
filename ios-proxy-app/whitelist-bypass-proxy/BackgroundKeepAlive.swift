import AVFoundation
import UIKit

class BackgroundKeepAlive {
    private var audioPlayer: AVAudioPlayer?

    func start() {
        let session = AVAudioSession.sharedInstance()
        try? session.setCategory(.playback, mode: .default, options: .mixWithOthers)
        try? session.setActive(true)

        let sampleRate = 44100.0
        let channels: UInt32 = 1
        let samples = Int(sampleRate)
        let bufferSize = samples * 2
        var wavData = Data()

        // RIFF header
        wavData.append(contentsOf: [0x52, 0x49, 0x46, 0x46]) // "RIFF"
        var chunkSize = UInt32(36 + bufferSize).littleEndian
        wavData.append(Data(bytes: &chunkSize, count: 4))
        wavData.append(contentsOf: [0x57, 0x41, 0x56, 0x45]) // "WAVE"

        // fmt subchunk
        wavData.append(contentsOf: [0x66, 0x6D, 0x74, 0x20]) // "fmt "
        var subchunk1Size = UInt32(16).littleEndian
        wavData.append(Data(bytes: &subchunk1Size, count: 4))
        var audioFormat = UInt16(1).littleEndian
        wavData.append(Data(bytes: &audioFormat, count: 2))
        var numChannels = UInt16(channels).littleEndian
        wavData.append(Data(bytes: &numChannels, count: 2))
        var sr = UInt32(sampleRate).littleEndian
        wavData.append(Data(bytes: &sr, count: 4))
        var byteRate = UInt32(sampleRate * Double(channels) * 2).littleEndian
        wavData.append(Data(bytes: &byteRate, count: 4))
        var blockAlign = UInt16(channels * 2).littleEndian
        wavData.append(Data(bytes: &blockAlign, count: 2))
        var bitsPerSample = UInt16(16).littleEndian
        wavData.append(Data(bytes: &bitsPerSample, count: 2))

        // data subchunk
        wavData.append(contentsOf: [0x64, 0x61, 0x74, 0x61]) // "data"
        var dataSize = UInt32(bufferSize).littleEndian
        wavData.append(Data(bytes: &dataSize, count: 4))
        wavData.append(Data(count: bufferSize)) // silence

        audioPlayer = try? AVAudioPlayer(data: wavData)
        audioPlayer?.numberOfLoops = -1
        audioPlayer?.volume = 0.0
        audioPlayer?.play()
    }

    func stop() {
        audioPlayer?.stop()
        audioPlayer = nil
        try? AVAudioSession.sharedInstance().setActive(false)
    }
}
