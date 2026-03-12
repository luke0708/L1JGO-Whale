package packet

import (
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// textDecoder 將客戶端編碼轉為 UTF-8（讀取封包用）
// textEncoder 將 UTF-8 轉為客戶端編碼（發送封包用）
var (
	textDecoder *encoding.Decoder
	textEncoder *encoding.Encoder
)

func init() {
	// 預設 Big5（繁體中文客戶端）
	textDecoder = traditionalchinese.Big5.NewDecoder()
	textEncoder = traditionalchinese.Big5.NewEncoder()
}

// InitEncoding 根據設定切換客戶端文字編碼。
// 支援 "MS950"（繁體 Big5）和 "GBK"（簡體）。
func InitEncoding(charset string) {
	switch charset {
	case "GBK", "gbk":
		textDecoder = simplifiedchinese.GBK.NewDecoder()
		textEncoder = simplifiedchinese.GBK.NewEncoder()
	default: // "MS950" 或其他 → Big5
		textDecoder = traditionalchinese.Big5.NewDecoder()
		textEncoder = traditionalchinese.Big5.NewEncoder()
	}
}

// EncodeString 將 UTF-8 字串轉為客戶端編碼的位元組。
func EncodeString(s string) []byte {
	encoded, err := textEncoder.Bytes([]byte(s))
	if err != nil {
		return []byte(s)
	}
	return encoded
}
