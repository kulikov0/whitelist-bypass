import SwiftUI

struct SheetScaffold<Content: View>: View {
    @ViewBuilder var content: Content

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                content
            }
            .padding(20)
        }
        .scrollDismissesKeyboardCompat()
        .background(Palette.surface.edgesIgnoringSafeArea(.all))
        .keyboardDoneToolbar()
        .sheetPresentationCompat()
    }
}

struct SheetHeader: View {
    let title: String
    var sub: String? = nil
    var destructive: Bool = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title)
                .font(.system(size: 19, weight: .bold))
                .foregroundColor(destructive ? Palette.accent : Palette.ink)
            if let sub {
                Text(sub)
                    .font(.system(size: 13))
                    .foregroundColor(Palette.ink3)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

struct SheetFooter: View {
    var cancelTitle: String = NSLocalizedString("sheet_cancel", comment: "")
    var saveTitle: String = NSLocalizedString("sheet_save_short", comment: "")
    var destructive: Bool = false
    let onCancel: () -> Void
    let onSave: () -> Void

    var body: some View {
        HStack(spacing: 10) {
            Button(action: onCancel) {
                Text(cancelTitle)
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundColor(Palette.ink2)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 13)
                    .overlay(
                        RoundedRectangle(cornerRadius: 12, style: .continuous)
                            .stroke(Palette.hairStrong, lineWidth: 1)
                    )
            }
            .buttonStyle(PlainButtonStyle())

            Button(action: onSave) {
                Text(saveTitle)
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundColor(.white)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 13)
                    .background(
                        RoundedRectangle(cornerRadius: 12, style: .continuous)
                            .fill(destructive ? Palette.error : Palette.accent)
                    )
            }
            .buttonStyle(PlainButtonStyle())
        }
        .padding(.top, 4)
    }
}

struct FieldBlock<Content: View>: View {
    let label: String
    @ViewBuilder var content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(label.uppercased())
                .font(Mono.label(10))
                .letterSpacing(1.2)
                .foregroundColor(Palette.ink3)
            content
        }
    }
}

struct SheetField: View {
    var placeholder: String = ""
    @Binding var text: String
    var mono: Bool = false
    var keyboard: UIKeyboardType = .default
    var secure: Bool = false

    var body: some View {
        Group {
            if secure {
                SecureField(placeholder, text: $text)
            } else {
                TextField(placeholder, text: $text)
            }
        }
        .font(mono ? Mono.label(13) : .system(size: 14))
        .foregroundColor(Palette.ink)
        .keyboardType(keyboard)
        .disableAutocorrection(true)
        .autocapitalization(.none)
        .padding(12)
        .background(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .fill(Palette.panel2)
        )
        .overlay(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .stroke(Palette.hair, lineWidth: 1)
        )
    }
}

struct OptionRow: View {
    let title: String
    var sub: String? = nil
    let selected: Bool
    var danger: Bool = false
    var trailingValue: String? = nil
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 12) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(title)
                        .font(.system(size: 14, weight: .semibold))
                        .foregroundColor(danger ? Palette.error : Palette.ink)
                    if let sub {
                        Text(sub)
                            .font(.system(size: 11))
                            .foregroundColor(Palette.ink3)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
                Spacer(minLength: 8)
                if let trailingValue {
                    Text(trailingValue)
                        .font(Mono.label(11))
                        .foregroundColor(Palette.ink3)
                }
                if selected {
                    Image(systemName: "checkmark")
                        .font(.system(size: 13, weight: .bold))
                        .foregroundColor(Palette.accent)
                }
            }
            .padding(.vertical, 12)
            .padding(.horizontal, 14)
            .frame(maxWidth: .infinity)
            .background(
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .fill(selected ? Palette.accentSoft : Palette.panel2)
            )
            .overlay(
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .stroke(selected ? Palette.accent.opacity(0.4) : Palette.hair, lineWidth: 1)
            )
        }
        .buttonStyle(PlainButtonStyle())
    }
}

struct ChoiceOption: Identifiable {
    let id: String
    let title: String
    var sub: String? = nil
}

struct ChoiceSheet: View {
    let title: String
    var sub: String? = nil
    let options: [ChoiceOption]
    let selectedId: String
    let onSelect: (String) -> Void
    @Environment(\.presentationMode) private var presentationMode

    var body: some View {
        SheetScaffold {
            SheetHeader(title: title, sub: sub)
            VStack(spacing: 8) {
                ForEach(options) { option in
                    OptionRow(title: option.title, sub: option.sub, selected: option.id == selectedId) {
                        onSelect(option.id)
                        presentationMode.wrappedValue.dismiss()
                    }
                }
            }
        }
    }
}

struct ConfirmSheet: View {
    let title: String
    var sub: String? = nil
    var confirmTitle: String
    var destructive: Bool = true
    let onConfirm: () -> Void
    @Environment(\.presentationMode) private var presentationMode

    var body: some View {
        SheetScaffold {
            SheetHeader(title: title, sub: sub, destructive: destructive)
            SheetFooter(saveTitle: confirmTitle, destructive: destructive,
                        onCancel: { presentationMode.wrappedValue.dismiss() },
                        onSave: { onConfirm(); presentationMode.wrappedValue.dismiss() })
        }
    }
}

struct InputSheet: View {
    let title: String
    var sub: String? = nil
    let fieldLabel: String
    let onSave: (String) -> Void
    @State private var value: String
    @Environment(\.presentationMode) private var presentationMode

    init(title: String, sub: String? = nil, fieldLabel: String, initial: String, onSave: @escaping (String) -> Void) {
        self.title = title
        self.sub = sub
        self.fieldLabel = fieldLabel
        self.onSave = onSave
        _value = State(initialValue: initial)
    }

    var body: some View {
        SheetScaffold {
            SheetHeader(title: title, sub: sub)
            FieldBlock(label: fieldLabel) {
                SheetField(text: $value)
            }
            SheetFooter(onCancel: { presentationMode.wrappedValue.dismiss() }, onSave: {
                let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
                guard !trimmed.isEmpty else { return }
                onSave(trimmed)
                presentationMode.wrappedValue.dismiss()
            })
        }
    }
}

struct MenuAction: Identifiable {
    let id: String
    let title: String
    let icon: String
    var value: String? = nil
    var danger: Bool = false
}

struct MenuSheet: View {
    let title: String
    var sub: String? = nil
    let actions: [MenuAction]
    let onSelect: (String) -> Void
    @Environment(\.presentationMode) private var presentationMode

    var body: some View {
        SheetScaffold {
            SheetHeader(title: title, sub: sub)
            VStack(spacing: 8) {
                ForEach(actions) { action in
                    Button {
                        onSelect(action.id)
                        presentationMode.wrappedValue.dismiss()
                    } label: {
                        HStack(spacing: 12) {
                            Image(systemName: action.icon)
                                .font(.system(size: 14))
                                .foregroundColor(action.danger ? Palette.error : Palette.ink2)
                                .frame(width: 26)
                            Text(action.title)
                                .font(.system(size: 14, weight: .semibold))
                                .foregroundColor(action.danger ? Palette.error : Palette.ink)
                            Spacer(minLength: 8)
                            if let value = action.value {
                                Text(value)
                                    .font(Mono.label(11))
                                    .foregroundColor(Palette.ink3)
                            }
                            Image(systemName: "chevron.right")
                                .font(.system(size: 11))
                                .foregroundColor(Palette.ink3)
                        }
                        .padding(.vertical, 12)
                        .padding(.horizontal, 14)
                        .frame(maxWidth: .infinity)
                        .background(
                            RoundedRectangle(cornerRadius: 12, style: .continuous)
                                .fill(Palette.panel2)
                        )
                        .overlay(
                            RoundedRectangle(cornerRadius: 12, style: .continuous)
                                .stroke(Palette.hair, lineWidth: 1)
                        )
                    }
                    .buttonStyle(PlainButtonStyle())
                }
            }
        }
    }
}

struct SheetToggleRow: View {
    let title: String
    var sub: String? = nil
    @Binding var isOn: Bool

    var body: some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundColor(Palette.ink)
                if let sub {
                    Text(sub)
                        .font(.system(size: 11))
                        .foregroundColor(Palette.ink3)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            Spacer(minLength: 8)
            Toggle("", isOn: $isOn).labelsHidden().accentToggle()
        }
        .padding(.vertical, 12)
        .padding(.horizontal, 14)
        .frame(maxWidth: .infinity)
        .background(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .fill(Palette.panel2)
        )
        .overlay(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .stroke(Palette.hair, lineWidth: 1)
        )
    }
}
