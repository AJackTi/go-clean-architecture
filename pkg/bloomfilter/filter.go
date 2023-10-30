package bloomfilter

import (
	"github.com/bits-and-blooms/bloom/v3"
)

type BloomFilter struct {
	*bloom.BloomFilter
}

const MAX_TOTAL = 30000

func New() *BloomFilter {
	bf := bloom.NewWithEstimates(MAX_TOTAL, 0.01)
	return &BloomFilter{bf}
}
