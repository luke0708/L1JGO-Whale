package packet

import (
	"encoding/binary"
	"sync"
)

// Writer builds an L1J server packet. All multi-byte writes are little-endian.
// The final Bytes() output is padded to an 8-byte boundary (matching ServerBasePacket.java).
type Writer struct {
	buf []byte
}

func NewWriter() *Writer {
	return &Writer{buf: make([]byte, 0, 64)}
}

func NewWriterWithOpcode(opcode byte) *Writer {
	w := &Writer{buf: make([]byte, 0, 64)}
	w.WriteC(opcode)
	return w
}

// WriteC writes 1 byte.
func (w *Writer) WriteC(v byte) {
	w.buf = append(w.buf, v)
}

// WriteH writes 2 bytes little-endian.
func (w *Writer) WriteH(v uint16) {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], v)
	w.buf = append(w.buf, b[:]...)
}

// WriteD writes 4 bytes little-endian (signed or unsigned via cast).
func (w *Writer) WriteD(v int32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(v))
	w.buf = append(w.buf, b[:]...)
}

// WriteDU writes 4 bytes little-endian unsigned.
func (w *Writer) WriteDU(v uint32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	w.buf = append(w.buf, b[:]...)
}

// WriteS 將 UTF-8 字串轉為客戶端編碼後寫入，以 null 結尾。
func (w *Writer) WriteS(s string) {
	if len(s) == 0 {
		w.buf = append(w.buf, 0) // just null terminator
		return
	}
	encoded, err := textEncoder.Bytes([]byte(s))
	if err != nil {
		// Fallback: 原始位元組（適用於純 ASCII）
		w.buf = append(w.buf, []byte(s)...)
	} else {
		w.buf = append(w.buf, encoded...)
	}
	w.buf = append(w.buf, 0) // null terminator
}

// WriteBytes writes raw bytes.
func (w *Writer) WriteBytes(b []byte) {
	w.buf = append(w.buf, b...)
}

// Bytes returns the packet content padded to an 8-byte boundary.
// This matches ServerBasePacket.getBytes() in Java:
//   "不足8組 補滿8組BYTE" — pad to 8-byte groups.
func (w *Writer) Bytes() []byte {
	size := len(w.buf)
	padding := size % 8
	if padding != 0 {
		for i := padding; i < 8; i++ {
			w.buf = append(w.buf, 0)
		}
	}
	return w.buf
}

// RawBytes returns the packet content without padding (for init packet).
func (w *Writer) RawBytes() []byte {
	return w.buf
}

// Len returns the current unpadded length.
func (w *Writer) Len() int {
	return len(w.buf)
}

// --- Writer 物件池 ---

var writerPool = sync.Pool{
	New: func() any { return &Writer{buf: make([]byte, 0, 64)} },
}

// AcquireWriter 從物件池取得 Writer 並寫入 opcode。
// 用完後應呼叫 BytesAndRelease 取得封包並歸還 Writer。
func AcquireWriter(opcode byte) *Writer {
	w := writerPool.Get().(*Writer)
	w.buf = w.buf[:0]
	w.WriteC(opcode)
	return w
}

// BytesAndRelease 回傳填充至 8 位元組邊界的封包副本，並將 Writer 歸還物件池。
// 回傳的 []byte 由呼叫方持有，與 Writer 的內部 buffer 無關。
func (w *Writer) BytesAndRelease() []byte {
	padded := w.Bytes()
	out := make([]byte, len(padded))
	copy(out, padded)
	w.buf = w.buf[:0]
	writerPool.Put(w)
	return out
}
