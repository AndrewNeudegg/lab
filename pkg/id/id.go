package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

func New(prefix string) string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s_%s", prefix, time.Now().UTC().Format("20060102_150405"))
	}
	return fmt.Sprintf("%s_%s_%s", prefix, time.Now().UTC().Format("20060102_150405"), hex.EncodeToString(b[:]))
}
