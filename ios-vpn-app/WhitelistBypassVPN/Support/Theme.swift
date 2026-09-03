import SwiftUI
import UIKit

extension UIColor {
    convenience init(hex: UInt32) {
        let a = CGFloat((hex >> 24) & 0xFF) / 255.0
        let r = CGFloat((hex >> 16) & 0xFF) / 255.0
        let g = CGFloat((hex >> 8) & 0xFF) / 255.0
        let b = CGFloat(hex & 0xFF) / 255.0
        self.init(red: r, green: g, blue: b, alpha: a)
    }

    static func dynamic(light: UInt32, dark: UInt32) -> UIColor {
        UIColor { traits in
            traits.userInterfaceStyle == .dark ? UIColor(hex: dark) : UIColor(hex: light)
        }
    }
}

enum Palette {
    static let accent = Color(UIColor.dynamic(light: 0xFF7B1C2A, dark: 0xFFD04C5E))
    static let accentSoft = Color(UIColor.dynamic(light: 0x1F7B1C2A, dark: 0x2ED04C5E))
    static let warn = Color(UIColor.dynamic(light: 0xFFB07A1D, dark: 0xFFE5B260))
    static let error = Color(UIColor.dynamic(light: 0xFFD32F2F, dark: 0xFFEF7A52))
    static let ink = Color(UIColor.dynamic(light: 0xFF0D0D0C, dark: 0xFFECECE8))
    static let ink2 = Color(UIColor.dynamic(light: 0xFF4A4946, dark: 0xFFA8A8A2))
    static let ink3 = Color(UIColor.dynamic(light: 0xFF8A8782, dark: 0xFF6A6A65))
    static let hair = Color(UIColor.dynamic(light: 0x140D0D0C, dark: 0x14ECECE8))
    static let hairStrong = Color(UIColor.dynamic(light: 0x290D0D0C, dark: 0x2EECECE8))
    static let panel = Color(UIColor.dynamic(light: 0xFFFFFFFF, dark: 0xFF131517))
    static let panel2 = Color(UIColor.dynamic(light: 0xFFF9F8F4, dark: 0xFF181B1E))
    static let surface = Color(UIColor.dynamic(light: 0xFFFFFFFF, dark: 0xFF0A0B0C))
}

enum Mono {
    static func label(_ size: CGFloat) -> Font {
        .system(size: size, weight: .regular, design: .monospaced)
    }
    static func bold(_ size: CGFloat) -> Font {
        .system(size: size, weight: .bold, design: .monospaced)
    }
}

struct StatCardBackground: ViewModifier {
    var radius: CGFloat = 18
    func body(content: Content) -> some View {
        content
            .background(
                RoundedRectangle(cornerRadius: radius, style: .continuous)
                    .fill(Palette.panel2)
            )
            .overlay(
                RoundedRectangle(cornerRadius: radius, style: .continuous)
                    .stroke(Palette.hair, lineWidth: 1)
            )
    }
}

extension View {
    func statCard(radius: CGFloat = 18) -> some View {
        modifier(StatCardBackground(radius: radius))
    }
}

enum ThemeMode: String, CaseIterable, Identifiable {
    case system = "SYSTEM"
    case light = "LIGHT"
    case dark = "DARK"

    var id: String { rawValue }

    var label: String {
        switch self {
        case .system: return NSLocalizedString("theme_system", comment: "")
        case .light: return NSLocalizedString("theme_light", comment: "")
        case .dark: return NSLocalizedString("theme_dark", comment: "")
        }
    }

    var colorScheme: ColorScheme? {
        switch self {
        case .system: return nil
        case .light: return .light
        case .dark: return .dark
        }
    }
}
