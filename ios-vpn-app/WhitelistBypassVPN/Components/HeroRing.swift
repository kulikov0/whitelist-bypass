import SwiftUI

struct HeroRing: View {
    let phase: ConnectionPhase

    @State private var pulsing = false

    private var active: Bool { phase == .connecting || phase == .connected }
    private var spinDuration: Double { phase == .connected ? 18 : 6 }

    var body: some View {
        ZStack {
            if active {
                TimelineView(.animation) { timeline in
                    Circle()
                        .fill(sweepGradient)
                        .frame(width: 224, height: 224)
                        .rotationEffect(.degrees(spinAngle(at: timeline.date)))
                        .blur(radius: phase == .connected ? 6 : 4)
                        .opacity(phase == .connected ? 0.9 : 0.85)
                }
            }

            Circle()
                .fill(Palette.panel2)
                .overlay(Circle().stroke(Palette.hair, lineWidth: 1))
                .frame(width: 192, height: 192)

            Circle()
                .stroke(active ? Palette.accentSoft : Palette.hairStrong,
                        style: StrokeStyle(lineWidth: 1, dash: [4, 3]))
                .frame(width: 220, height: 220)

            if phase == .connecting {
                Circle()
                    .fill(Palette.accentSoft)
                    .frame(width: 168, height: 168)
                    .scaleEffect(pulsing ? 1.18 : 1.0)
                    .opacity(pulsing ? 0.0 : 0.7)
            }

            button
        }
        .frame(width: 248, height: 248)
        .onAppear { syncPulse() }
        .onDisappear { stopPulse() }
        .onChange(of: phase) { _ in syncPulse() }
    }

    private func spinAngle(at date: Date) -> Double {
        (date.timeIntervalSinceReferenceDate / spinDuration).truncatingRemainder(dividingBy: 1) * 360
    }

    private var sweepGradient: AngularGradient {
        switch phase {
        case .connected:
            return AngularGradient(
                gradient: Gradient(stops: [
                    .init(color: .clear, location: 0),
                    .init(color: Palette.accent, location: 0.3),
                    .init(color: Palette.accent, location: 0.7),
                    .init(color: .clear, location: 1),
                ]),
                center: .center
            )
        default:
            return AngularGradient(
                gradient: Gradient(stops: [
                    .init(color: .clear, location: 0),
                    .init(color: Palette.accentSoft, location: 0.35),
                    .init(color: .clear, location: 0.6),
                    .init(color: .clear, location: 1),
                ]),
                center: .center
            )
        }
    }

    private func syncPulse() {
        if phase == .connecting {
            startPulse()
        } else {
            stopPulse()
        }
    }

    private func startPulse() {
        pulsing = false
        withAnimation(.easeOut(duration: 1.6).repeatForever(autoreverses: false)) {
            pulsing = true
        }
    }

    private func stopPulse() {
        withAnimation(.linear(duration: 0)) {
            pulsing = false
        }
    }

    @ViewBuilder
    private var button: some View {
        ZStack {
            switch phase {
            case .connected:
                Circle().fill(Palette.accentSoft).frame(width: 168, height: 168)
                Circle().fill(Palette.accent).frame(width: 152, height: 152)
            case .connecting:
                Circle().fill(Palette.accentSoft).frame(width: 168, height: 168)
                Circle()
                    .fill(Palette.panel)
                    .overlay(Circle().stroke(Palette.accent, lineWidth: 1))
                    .frame(width: 156, height: 156)
            case .disconnected, .failed:
                Circle()
                    .fill(Palette.panel)
                    .overlay(Circle().stroke(Palette.hairStrong, lineWidth: 1))
                    .frame(width: 168, height: 168)
            }

            VStack(spacing: 12) {
                Image(systemName: "power")
                    .font(.system(size: 40, weight: .regular))
                Text(buttonLabel.uppercased())
                    .font(Mono.label(11))
                    .tracking(2.4)
            }
            .foregroundStyle(labelColor)
        }
        .frame(width: 168, height: 168)
        .contentShape(Circle())
    }

    private var buttonLabel: String {
        switch phase {
        case .connected: return NSLocalizedString("btn_disconnect", comment: "")
        case .connecting: return NSLocalizedString("btn_cancel", comment: "")
        case .disconnected, .failed: return NSLocalizedString("btn_connect", comment: "")
        }
    }

    private var labelColor: Color {
        switch phase {
        case .connected: return Palette.panel
        case .connecting: return Palette.accent
        case .disconnected, .failed: return Palette.ink3
        }
    }
}
