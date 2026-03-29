package bypass.whitelist.util

private val reUrl = Regex("(https?://[^/]+/)(.+)")
private val reIp4 = Regex("\\d+\\.\\d+\\.\\d+\\.\\d+")
private val reIp6 = Regex("[0-9a-fA-F]{1,4}(?::[0-9a-fA-F]{1,4}){2,7}|(?:[0-9a-fA-F]{1,4}:){1,6}:[0-9a-fA-F]{1,4}|::(?:[0-9a-fA-F]{1,4}:){0,5}[0-9a-fA-F]{1,4}")

fun maskUrl(url: String): String {
    return url.replace(reUrl) { m ->
        m.groupValues[1] + m.groupValues[2].take(4) + "***"
    }.replace(reIp4) { m ->
        val p = m.value.split(".")
        "${p[0]}.${p[1]}.x.x"
    }.replace(reIp6, "x::x")
}
