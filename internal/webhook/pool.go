package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"hash"
	"sync"
)

var bufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 512)
		return &b
	},
}

var hexPool = sync.Pool{
	New: func() any {
		b := make([]byte, 64)
		return &b
	},
}

var hmacPool sync.Pool

func InitHMACPool(secret []byte) {
	key := make([]byte, len(secret))
	copy(key, secret)
	hmacPool = sync.Pool{
		New: func() any {
			return hmac.New(sha256.New, key)
		},
	}
}

func acquireBuffer() *[]byte {
	bp := bufPool.Get().(*[]byte)
	*bp = (*bp)[:0]
	return bp
}

func releaseBuffer(bp *[]byte) {
	if cap(*bp) <= 4096 {
		bufPool.Put(bp)
	}
}

func acquireHMAC() hash.Hash {
	return hmacPool.Get().(hash.Hash)
}

func releaseHMAC(h hash.Hash) {
	hmacPool.Put(h)
}

func acquireHex() *[]byte {
	return hexPool.Get().(*[]byte)
}

func releaseHex(bp *[]byte) {
	hexPool.Put(bp)
}
