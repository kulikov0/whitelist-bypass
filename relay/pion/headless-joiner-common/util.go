package joiner

import (
	"strings"
)

func TmParseMids(sdp string) (audioMid, videoMid, screenMid string) {
	var media string
	var videoIndex int
	for _, line := range strings.Split(sdp, "\r\n") {
		if strings.HasPrefix(line, "m=audio") {
			media = "audio"
		} else if strings.HasPrefix(line, "m=video") {
			media = "video"
			videoIndex++
		}
		if strings.HasPrefix(line, "a=mid:") {
			mid := strings.TrimPrefix(line, "a=mid:")
			if media == "audio" && audioMid == "" {
				audioMid = mid
			} else if media == "video" {
				if videoIndex == 1 && videoMid == "" {
					videoMid = mid
				} else if videoIndex == 2 && screenMid == "" {
					screenMid = mid
				}
			}
		}
	}
	return
}
