package main

import (
	"testing"
)

func TestBitStrFromBytes(t *testing.T) {
	e1 := bitStr{[]uint32{0x12345678, 0x9abcdef0}, 64}
	b1 := []byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0}
	a1 := bitStrFromBytes(b1, 64)
	if !e1.Equal(a1) {
		t.Errorf("BitStr(%v) = %v should equal to %v", b1, a1, e1)
	}

	e2 := bitStr{[]uint32{0x12345678, 0x9abcdef0, 0x87654321}, 78}
	b2 := []byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 0x87, 0x67}
	a2 := bitStrFromBytes(b2, 78)
	if !e2.Equal(a2) {
		t.Errorf("BitStr(%v) = %v should equal to %v", b2, a2, e2)
	}
}

func TestBitStrEqual(t *testing.T) {
	a := bitStr{[]uint32{0x12345678, 0x9abcdef0}, 64}
	b := bitStr{[]uint32{0x12345678, 0x9abcdef0}, 60}
	c := bitStr{[]uint32{0x12345678, 0x9abcdef0}, 60}
	d := bitStr{[]uint32{0x12345678, 0x9abcdeff}, 60}
	e := bitStr{[]uint32{0x12345678, 0x9abcde00}, 60}
	f := bitStr{[]uint32{0x12345670, 0x9abcdef0}, 60}
	g := bitStr{}

	if a.Equal(b) {
		t.Errorf("%v should not equal to %v", a, b)
	}
	if !b.Equal(c) {
		t.Errorf("%v should equal to %v", b, c)
	}
	if !b.Equal(d) {
		t.Errorf("%v should equal to %v", b, d)
	}
	if b.Equal(e) {
		t.Errorf("%v should not equal to %v", b, e)
	}
	if b.Equal(f) {
		t.Errorf("%v should not equal to %v", b, f)
	}
	if f.Equal(g) {
		t.Errorf("%v should not equal to %v", f, g)
	}
	if !g.Equal(g) {
		t.Errorf("%v should equal to %v", g, g)
	}
}

func TestBitStrBit(t *testing.T) {
	s := bitStr{[]uint32{0x12345678, 0x9abcdef0}, 64}
	x := 0x9abcdef0
	for i := 63; i >= 32; i-- {
		if s.Bit(uint(i)) != (x&0x1 == 0x1) {
			t.Fatalf("mismatch at bit %d", i)
		}
		x >>= 1
	}
	x = 0x12345678
	for i := 31; i >= 0; i-- {
		if s.Bit(uint(i)) != (x&0x1 == 0x1) {
			t.Fatalf("mismatch at bit %d", i)
		}
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

	if !a.Equal(s.Substr(0, 64)) {
		t.Errorf("a != s[0,64] <--> %v != %v", a, s.Substr(0, 64))
	}
	if !b.Equal(s.Substr(32, 60)) {
		t.Errorf("b != s[32,60] <--> %v != %v", b, s.Substr(32, 60))
	}
	if !c.Equal(s.Substr(48, 60)) {
		t.Errorf("c != s[48,60] <--> %v != %v", c, s.Substr(48, 60))
	}
	if !d.Equal(s.Substr(1, 0)) {
		t.Errorf("d != s[1,0] <--> %v != %v", d, s.Substr(1, 0))
	}
	if !d.Equal(s.Substr(70, 0)) {
		t.Errorf("d != s[70,0] <--> %v != %v", d, s.Substr(70, 0))
	}
	if !e.Equal(s.Substr(88, 40)) {
		t.Errorf("e != s[88,40] <--> %v != %v", e, s.Substr(88, 40))
	}
}

func TestBitStrCommPfxLen(t *testing.T) {
	a := bitStr{[]uint32{0x12345678, 0xaaaaaaaa, 0x9abcdef0}, 96}
	b := bitStr{}
	c := bitStr{[]uint32{0x12345678, 0xaaaaaaaa}, 60}
	d := bitStr{[]uint32{0x12345678, 0xaaaafaaa, 0x9abcdef0, 0x55555555}, 128}

	if a.CommPfxLen(b) != 0 || b.CommPfxLen(a) != 0 {
		t.Errorf("a CPL b == %d", a.CommPfxLen(b))
	}
	if a.CommPfxLen(c) != 60 || c.CommPfxLen(a) != 60 {
		t.Errorf("a CPL c == %d", a.CommPfxLen(c))
	}
	if a.CommPfxLen(d) != 49 || d.CommPfxLen(a) != 49 {
		t.Errorf("a CPL d == %d", a.CommPfxLen(d))
	}
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
		if !ok {
			t.Errorf("%v not found", m.pfx)
		} else if result != m.data {
			t.Errorf("m[%v] = %v != %v", m.pfx, result, m.data)
		}
	}

	for _, q := range queries {
		result, ok := root.FindPrefix(q.str).(int)
		if q.result == nil {
			if ok {
				t.Errorf("m[%v] = %v should not be found", q.str, result)
			}
		} else {
			if q.result.(int) != result {
				t.Errorf("m[%v] = %v != %v", q.str, result, q.result)
			}
		}
	}
}
