package main

import "encoding/json"

type kbAction struct {
	Type    string `json:"type"`
	Label   string `json:"label"`
	Payload string `json:"payload"`
}

type kbButton struct {
	Action kbAction `json:"action"`
}

type keyboard struct {
	OneTime bool         `json:"one_time"`
	Buttons [][]kbButton `json:"buttons"`
}

func platformSquare(platform string) string {
	switch platform {
	case "vk":
		return "🟦"
	case "tm", "telemost":
		return "🟥"
	case "wb", "wbstream":
		return "🟪"
	case "dion":
		return "🟩"
	case "bitrix":
		return "🟧"
	}
	return ""
}

func mainMenuKeyboard() string {
	kb := keyboard{
		OneTime: false,
		Buttons: [][]kbButton{
			{
				{Action: kbAction{Type: "text", Label: "🟦 VK", Payload: `{"cmd":"vk"}`}},
				{Action: kbAction{Type: "text", Label: "🟥 Telemost", Payload: `{"cmd":"tm"}`}},
			},
			{
				{Action: kbAction{Type: "text", Label: "🟪 WBStream", Payload: `{"cmd":"wb"}`}},
				{Action: kbAction{Type: "text", Label: "🟩 DION", Payload: `{"cmd":"dion"}`}},
			},
			{
				{Action: kbAction{Type: "text", Label: "🟧 Bitrix", Payload: `{"cmd":"bitrix"}`}},
			},
			{
				{Action: kbAction{Type: "text", Label: "🔗 Join by link", Payload: `{"cmd":"join-prompt"}`}},
			},
			{
				{Action: kbAction{Type: "text", Label: "📋 Active sessions", Payload: `{"cmd":"list"}`}},
			},
		},
	}
	data, _ := json.Marshal(kb)
	return string(data)
}

func waitingKeyboard() string {
	kb := keyboard{
		OneTime: false,
		Buttons: [][]kbButton{
			{
				{Action: kbAction{Type: "text", Label: "◀️ Back", Payload: `{"cmd":"menu"}`}},
			},
		},
	}
	data, _ := json.Marshal(kb)
	return string(data)
}

func sessionsKeyboard(sessions []*session) string {
	rows := make([][]kbButton, 0, len(sessions)+1)
	for _, s := range sessions {
		payload, _ := json.Marshal(map[string]string{"cmd": "close", "id": s.id})
		label := platformSquare(s.platform) + " " + s.platform + " " + s.id + " 🟢"
		rows = append(rows, []kbButton{
			{Action: kbAction{Type: "text", Label: label, Payload: string(payload)}},
		})
	}
	rows = append(rows, []kbButton{
		{Action: kbAction{Type: "text", Label: "◀️ Back", Payload: `{"cmd":"menu"}`}},
	})
	kb := keyboard{OneTime: false, Buttons: rows}
	data, _ := json.Marshal(kb)
	return string(data)
}
