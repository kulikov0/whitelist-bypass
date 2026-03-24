package bypass.whitelist

enum class TunnelMode(val label: String, val relayArg: String, val isPion: Boolean) {
    DC("DC", "dc", false),
    PION_VIDEO("Video", "video", true);

    fun relayMode(isTelemost: Boolean): String {
        if (!isPion) return "dc-joiner"
        val platform = if (isTelemost) "telemost" else "vk"
        return "$platform-$relayArg-joiner"
    }
}