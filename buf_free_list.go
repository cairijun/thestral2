package main

import "sync"

// GlobalBufPool is a globally available BufFreeList for buffers of sizes
// between 16B and 16K.
var GlobalBufPool = NewBufFreeList(4, 16) // 16B -> 64K

// BufFreeList is a bucketing free list for byte buffers.
type BufFreeList struct {
	minN  uint
	maxN  uint
	pools []*sync.Pool
}

// NewBufFreeList creates a BufFreeList for buffers of sizes in
// [2^minN, 2^maxN] bytes.
func NewBufFreeList(minN, maxN uint) *BufFreeList {
	if maxN <= 0 {
		panic("maxN must be greater than 0")
	}
	if minN > maxN {
		panic("maxN must be greater than or equal to minN")
	}

	l := &BufFreeList{
		minN: minN, maxN: maxN, pools: make([]*sync.Pool, maxN-minN+1),
	}
	for i := minN; i <= maxN; i++ {
		size := 1 << i
		l.pools[i-minN] = &sync.Pool{
			New: func() interface{} {
				return make([]byte, size)
			},
		}
	}
	return l
}

// Get return a byte slice of the given size.
func (l *BufFreeList) Get(size uint) []byte {
	if size == 0 {
		return nil
	}
	if size > (1 << l.maxN) {
		return make([]byte, size)
	}
	p := l.pools[l.getBucketIdx(size)]
	return p.Get().([]byte)[:size]
}

// Free puts back the given byte slice to the free list.
func (l *BufFreeList) Free(buf []byte) {
	size := cap(buf)
	if size > 0 && size <= (1<<l.maxN) {
		idx := l.getBucketIdx(uint(size))
		l.pools[idx].Put(buf)
	}
}

func (l *BufFreeList) getBucketIdx(size uint) uint {
	size = (size - 1) >> l.minN
	idx := uint(0)
	for size != 0 {
		idx++
		size >>= 1
	}
	return idx
}
