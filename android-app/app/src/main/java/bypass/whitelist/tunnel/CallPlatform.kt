package bypass.whitelist.tunnel

enum class CallPlatform(val id: String, val urlMarker: String) {
    VK("vk", ""),
    TELEMOST("telemost", "telemost.yandex");

    companion object {
        fun fromUrl(url: String): CallPlatform =
            if (url.contains(TELEMOST.urlMarker)) TELEMOST else VK
    }
}
