package lib

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBitStrFromBytes(t *testing.T) {
	e1 := bitStr{[]uint32{0x12345678, 0x9abcdef0}, 64}
	b1 := []byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0}
	a1 := bitStrFromBytes(b1, 64)
	assert.True(t, e1.Equal(a1))

	e2 := bitStr{[]uint32{0x12345678, 0x9abcdef0, 0x87654321}, 78}
	b2 := []byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 0x87, 0x67}
	a2 := bitStrFromBytes(b2, 78)
	assert.True(t, e2.Equal(a2))
}

func TestBitStrEqual(t *testing.T) {
	a := bitStr{[]uint32{0x12345678, 0x9abcdef0}, 64}
	b := bitStr{[]uint32{0x12345678, 0x9abcdef0}, 60}
	c := bitStr{[]uint32{0x12345678, 0x9abcdef0}, 60}
	d := bitStr{[]uint32{0x12345678, 0x9abcdeff}, 60}
	e := bitStr{[]uint32{0x12345678, 0x9abcde00}, 60}
	f := bitStr{[]uint32{0x12345670, 0x9abcdef0}, 60}
	g := bitStr{}

	assert.False(t, a.Equal(b))
	assert.True(t, b.Equal(c))
	assert.True(t, b.Equal(d))
	assert.False(t, b.Equal(e))
	assert.False(t, b.Equal(f))
	assert.False(t, f.Equal(g))
	assert.True(t, g.Equal(g))
}

func TestBitStrBit(t *testing.T) {
	s := bitStr{[]uint32{0x12345678, 0x9abcdef0}, 64}
	x := 0x9abcdef0
	for i := 63; i >= 32; i-- {
		assert.Equal(t, x&0x1 == 0x1, s.Bit(uint(i)), "mismatch at bit %d", i)
		x >>= 1
	}
	x = 0x12345678
	for i := 31; i >= 0; i-- {
		assert.Equal(t, x&0x1 == 0x1, s.Bit(uint(i)), "mismatch at bit %d", i)
		x >>= 1
	}
}

func TestBitStrSubstr(t *testing.T) {
	s := bitStr{[]uint32{0x12345678, 0xaaaaaaaa, 0x9abcdef0, 0x55555555}, 128}
	a := bitStr{[]uint32{0x12345678, 0xaaaaaaaa}, 64}
	b := bitStr{[]uint32{0xaaaaaaaa, 0x9abcdef0}, 60}
	c := bitStr{[]uint32{0xaaaa9abc, 0xdef05555}, 60}
	d := bitStr{}
	e := bitStr{[]uint32{0xf0555555, 0x55000000}, 40}

	assert.True(t, a.Equal(s.Substr(0, 64)))
	assert.True(t, b.Equal(s.Substr(32, 60)))
	assert.True(t, c.Equal(s.Substr(48, 60)))
	assert.True(t, d.Equal(s.Substr(1, 0)))
	assert.True(t, d.Equal(s.Substr(70, 0)))
	assert.True(t, e.Equal(s.Substr(88, 40)))
}

func TestBitStrCommPfxLen(t *testing.T) {
	a := bitStr{[]uint32{0x12345678, 0xaaaaaaaa, 0x9abcdef0}, 96}
	b := bitStr{}
	c := bitStr{[]uint32{0x12345678, 0xaaaaaaaa}, 60}
	d := bitStr{[]uint32{0x12345678, 0xaaaafaaa, 0x9abcdef0, 0x55555555}, 128}

	assert.EqualValues(t, 0, a.CommPfxLen(b))
	assert.EqualValues(t, 0, b.CommPfxLen(a))
	assert.EqualValues(t, 60, a.CommPfxLen(c))
	assert.EqualValues(t, 60, c.CommPfxLen(a))
	assert.EqualValues(t, 49, a.CommPfxLen(d))
	assert.EqualValues(t, 49, d.CommPfxLen(a))
}

func TestBinRadixTree(t *testing.T) {
	mappings := []struct {
		pfx  bitStr
		data int
	}{
		{bitStr{[]uint32{0xc0a80000}, 16}, 1},
		{bitStr{[]uint32{0xc0a80000}, 24}, 11},
		{bitStr{[]uint32{0xc0a80000}, 28}, 12},
		{bitStr{[]uint32{0xac100000}, 12}, 2},
		{bitStr{[]uint32{0xc0a86500}, 24}, 13},
		{bitStr{[]uint32{0xc0a865f0}, 28}, 14},
		{bitStr{[]uint32{0xc0a86680}, 25}, 15},
		{bitStr{[]uint32{0x5aef002b, 0x7c1f0000}, 48}, 3},
		{bitStr{[]uint32{0x5aef002b, 0x00000000}, 32}, 4},
		{bitStr{[]uint32{0x5aef002b, 0x7c1fabc0}, 58}, 31},
		{bitStr{[]uint32{0x5aef002b, 0x7c1fab80}, 58}, 32},
	}

	queries := []struct {
		str    bitStr
		result interface{}
	}{
		{bitStr{[]uint32{0x5aef0020}, 32}, nil},
		{bitStr{[]uint32{0x5aef002c, 0x7c1fabc0}, 57}, nil},
		{bitStr{[]uint32{0xc0a8ffff}, 32}, 1},
		{bitStr{[]uint32{0xc0a800ff}, 32}, 11},
		{bitStr{[]uint32{0xc0a8000f}, 32}, 12},
		{bitStr{[]uint32{0xc0a80100}, 32}, 1},
		{bitStr{[]uint32{0xac10ffff}, 32}, 2},
		{bitStr{[]uint32{0xc0a865af}, 32}, 13},
		{bitStr{[]uint32{0xc0a865ff}, 32}, 14},
		{bitStr{[]uint32{0xc0a8668f}, 32}, 15},
		{bitStr{[]uint32{0x5aef002b, 0x7c1fffff}, 64}, 3},
		{bitStr{[]uint32{0x5aef002b, 0x7c1cabc0}, 57}, 4},
		{bitStr{[]uint32{0x5aef002b, 0xffffffff}, 64}, 4},
		{bitStr{[]uint32{0x5aef002b, 0x7c1fabcf}, 64}, 31},
		{bitStr{[]uint32{0x5aef002b, 0x7c1fab8f}, 64}, 32},
	}

	root := &brtNode{}
	for _, m := range mappings {
		root.Insert(m.pfx, m.data)
	}

	for _, m := range mappings {
		result, ok := root.FindPrefix(m.pfx).(int)
		assert.True(t, ok)
		assert.Equal(t, m.data, result)
	}

	for _, q := range queries {
		result, ok := root.FindPrefix(q.str).(int)
		if q.result == nil {
			assert.False(t, ok)
		} else {
			assert.Equal(t, result, q.result.(int))
		}
	}
}
