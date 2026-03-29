package webhook

import (
	"encoding/hex"
)

func Sign(dst []byte, payload []byte) int {
	h := acquireHMAC()
	h.Reset()
	h.Write(payload)

	var mac [32]byte
	h.Sum(mac[:0])
	releaseHMAC(h)

	hex.Encode(dst, mac[:])
	return 64
}
