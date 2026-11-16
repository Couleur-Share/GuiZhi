package parity

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// 对照场景里用到的文案。React 采集器（scripts/parity/scenes/react-scenes.mjs）与 Viva 场景共用同一份文件，
// 避免两边各写一遍、悄悄漂移——文案不同，换行位置与外框就不同，对照结果没有意义。

//go:embed testdata/toast-content.json
var toastContentJSON []byte

// ToastTexts 是提示对照场景的文案。
type ToastTexts struct {
	Success string `json:"success"`
	Info    string `json:"info"`
	Warning string `json:"warning"`
	Error   string `json:"error"`
	Undo    string `json:"undo"`
	Detail  string `json:"detail"`
}

// ToastContent 读取提示对照场景的文案；文件随包嵌入，解析失败是程序错误。
func ToastContent() ToastTexts {
	var t ToastTexts
	if err := json.Unmarshal(toastContentJSON, &t); err != nil {
		panic(fmt.Sprintf("parity: testdata/toast-content.json 无法解析：%v", err))
	}
	return t
}
