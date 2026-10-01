package service

import (
	"github.com/cespare/xxhash/v2"
	"strconv"
)

// shortHash 使用 XXHash64 + Base36 生成短 hash（16 字符）
// XXHash64 比 SHA256 快约 10 倍，Base36 比 Hex 短约 20%
func shortHash(data []byte) string {
	h := xxhash.Sum64(data)
	return strconv.FormatUint(h, 36)
}
